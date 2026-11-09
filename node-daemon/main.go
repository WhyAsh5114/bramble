// Command bramble is the one binary a person installs: it runs this device's
// node and serves the local web UI where they create a tailnet, join one, and
// manage devices and services. `bramble relay` runs a relay with the same
// binary.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/WhyAsh5114/bramble/brambled"
	"github.com/WhyAsh5114/bramble/node-daemon/internal/daemon"
	"github.com/WhyAsh5114/bramble/node-daemon/ui"
	"github.com/WhyAsh5114/bramble/relay/relaysrv"
)

var version = "dev"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "up":
		err = runUp(os.Args[2:])
	case "down":
		err = runDown()
	case "status":
		err = runStatus()
	case "open":
		err = runOpen()
	case "uninstall":
		err = runUninstall(os.Args[2:])
	case "relay":
		os.Exit(relaysrv.Main(os.Args[2:], version))
	case "node":
		// The node daemon on its own, configured by flags and environment (brambled).
		os.Exit(brambled.Main(os.Args[2:]))
	case "version", "-v", "--version":
		fmt.Println(version)
	case "help", "-h", "--help":
		usage()
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "bramble:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `usage:
  bramble up [-port 7777] [-d] [-open=true] [-tun] [-listen-port 41641]
                       start this device and its local UI
  bramble down         stop it
  bramble status       show this device's state
  bramble open         open the local UI
  bramble relay [...]  run a relay (bramble relay -h for options; bramble relay pin prints its key pin)
  bramble uninstall    stop and delete this device's key and state
  bramble version
`)
}

func store() (*daemon.Store, error) {
	dir, err := daemon.DefaultDir()
	if err != nil {
		return nil, err
	}
	return &daemon.Store{Dir: dir}, nil
}

func running(s *daemon.Store) string {
	u := s.UIURL()
	if u == "" {
		return ""
	}
	c := &http.Client{Timeout: 2 * time.Second}
	resp, err := c.Get(u + "/api/state")
	if err != nil {
		return ""
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	return u
}

func runUp(args []string) error {
	fs := flag.NewFlagSet("up", flag.ContinueOnError)
	port := fs.Int("port", 7777, "preferred local UI port; the next free port is used if taken")
	detach := fs.Bool("d", false, "run in the background and return once the UI answers")
	openUI := fs.Bool("open", true, "open the UI in a browser")
	tun := fs.Bool("tun", false, "use a real WireGuard interface (needs root) instead of userspace networking")
	listenPort := fs.Uint("listen-port", 41641, "UDP port for WireGuard")
	if err := fs.Parse(args); err != nil {
		return err
	}
	s, err := store()
	if err != nil {
		return err
	}
	if err := s.Ensure(); err != nil {
		return err
	}
	if u := running(s); u != "" {
		fmt.Printf("Bramble is already running: %s\n", u)
		if *openUI {
			openBrowser(u)
		}
		return nil
	}
	if *detach {
		return detachUp(s, args, *openUI)
	}

	logger := log.New(os.Stderr, "bramble: ", log.LstdFlags)
	d := &daemon.Daemon{Store: s, Logger: logger, Version: version, TUN: *tun, ListenPort: uint16(*listenPort)}
	if err := d.Init(); err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ln, boundPort, err := daemon.Listen(*port)
	if err != nil {
		return err
	}
	srv := &daemon.Server{Daemon: d, UI: ui.FS(), Port: boundPort}
	httpSrv := &http.Server{Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second}
	uiURL := fmt.Sprintf("http://127.0.0.1:%d", boundPort)
	if err := s.SaveRuntime(uiURL, os.Getpid()); err != nil {
		return err
	}
	defer s.ClearRuntime()
	go func() {
		if err := httpSrv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Printf("UI server stopped: %v", err)
			stop()
		}
	}()
	fmt.Printf("Bramble is running: %s\n", uiURL)
	if !ui.Built() {
		logger.Printf("serving the placeholder page (no UI build embedded)")
	}
	if *openUI {
		openBrowser(uiURL)
	}
	d.Run(ctx)
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = httpSrv.Shutdown(shutdownCtx)
	return nil
}

func detachUp(s *daemon.Store, args []string, openUI bool) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	var childArgs []string
	for _, a := range args {
		if a != "-d" && a != "--d" && a != "-d=true" {
			childArgs = append(childArgs, a)
		}
	}
	childArgs = append([]string{"up", "-open=false"}, childArgs...)
	logFile, err := s.OpenLog("bramble.log")
	if err != nil {
		return err
	}
	defer logFile.Close()
	cmd := exec.Command(self, childArgs...)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if u := running(s); u != "" {
			fmt.Printf("Bramble is running: %s\n", u)
			if openUI {
				openBrowser(u)
			}
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("bramble did not start; see %s/logs/bramble.log", s.Dir)
}

func openBrowser(u string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", u)
	case "linux":
		if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
			return
		}
		cmd = exec.Command("xdg-open", u)
	default:
		return
	}
	_ = cmd.Start()
}

func runDown() error {
	s, err := store()
	if err != nil {
		return err
	}
	pid := s.PID()
	if pid == 0 {
		fmt.Println("Bramble is not running.")
		return nil
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	if err := p.Signal(syscall.SIGTERM); err != nil {
		s.ClearRuntime()
		fmt.Println("Bramble is not running.")
		return nil
	}
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if p.Signal(syscall.Signal(0)) != nil {
			fmt.Println("Bramble stopped.")
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("bramble (pid %d) did not stop within 30 s", pid)
}

func runStatus() error {
	s, err := store()
	if err != nil {
		return err
	}
	u := running(s)
	if u == "" {
		fmt.Println("Bramble is not running. Start it with `bramble up`.")
		return nil
	}
	resp, err := http.Get(u + "/api/state")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var st daemon.State
	if err := json.NewDecoder(resp.Body).Decode(&st); err != nil {
		return err
	}
	fmt.Printf("UI:          %s\n", u)
	fmt.Printf("Key:         %s\n", st.Fingerprint)
	fmt.Printf("Status:      %s\n", st.Phase)
	if st.Tailnet != nil {
		fmt.Printf("Tailnet:     %s\n", st.Tailnet.Name)
		fmt.Printf("Device:      %s\n", st.Label)
	}
	if st.Node != nil && st.Node.Address != "" {
		fmt.Printf("Address:     %s\n", st.Node.Address)
	}
	if st.Phase == daemon.PhaseWaiting {
		fmt.Printf("Join code:   %s\n", st.JoinCode)
	}
	if st.Problem != "" {
		fmt.Printf("Problem:     %s\n", st.Problem)
	}
	return nil
}

func runOpen() error {
	s, err := store()
	if err != nil {
		return err
	}
	u := running(s)
	if u == "" {
		return fmt.Errorf("bramble is not running; start it with `bramble up`")
	}
	fmt.Println(u)
	openBrowser(u)
	return nil
}

func runUninstall(args []string) error {
	fs := flag.NewFlagSet("uninstall", flag.ContinueOnError)
	yes := fs.Bool("y", false, "do not ask for confirmation")
	if err := fs.Parse(args); err != nil {
		return err
	}
	s, err := store()
	if err != nil {
		return err
	}
	if !*yes {
		fmt.Printf("This stops Bramble and deletes %s, including this device's key. Continue? [y/N] ", s.Dir)
		line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		if a := strings.ToLower(strings.TrimSpace(line)); a != "y" && a != "yes" {
			fmt.Println("Nothing removed.")
			return nil
		}
	}
	if err := runDown(); err != nil {
		return err
	}
	if err := s.RemoveAll(); err != nil {
		return err
	}
	fmt.Printf("Removed %s.\n", s.Dir)
	fmt.Println("The device stays enrolled in its tailnet until an admin revokes it there.")
	return nil
}
