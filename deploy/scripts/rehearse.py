#!/usr/bin/env python3
"""Rehearse demo beats 1, 2, 3 and 5 on an existing pair, or simulate offline."""

import argparse
import json
import math
import os
import re
import shlex
import subprocess
import sys
import time
import urllib.error
import urllib.parse
import urllib.request


class Failure(Exception):
    """A safe diagnostic that contains no server body, environment or command output."""


def require(condition, message):
    if not condition:
        raise Failure(message)


def origin(value):
    url = urllib.parse.urlsplit(value)
    if (url.username or url.password or url.query or url.fragment or url.path not in ('', '/')
            or not url.hostname or not (url.scheme == 'https'
            or (url.scheme == 'http' and url.hostname == '127.0.0.1'))):
        raise argparse.ArgumentTypeError('Use an HTTPS origin, or HTTP on 127.0.0.1.')
    return value.rstrip('/')


def positive(value):
    number = float(value)
    if not math.isfinite(number) or number <= 0:
        raise argparse.ArgumentTypeError('Expected a positive finite number.')
    return number


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, *_):
        return None


class Pair:
    def __init__(self, args):
        self.args = args
        require(os.environ.get('APPROVAL_DEMO_TOKEN'), 'APPROVAL_DEMO_TOKEN is missing.')
        self.http = urllib.request.build_opener(NoRedirect(), urllib.request.ProxyHandler({}))

    def api(self, method, path, body=None):
        data = json.dumps(body or {}).encode() if method == 'POST' else None
        request = urllib.request.Request(self.args.base + path, data=data, method=method, headers={
            'Authorization': 'Bearer ' + os.environ['APPROVAL_DEMO_TOKEN'],
            'Content-Type': 'application/json',
        })
        try:
            with self.http.open(request, timeout=self.args.timeout) as response:
                return json.load(response)
        except urllib.error.HTTPError as error:
            raise Failure('Approval API returned HTTP ' + str(error.code) + '; no write is retried.') from None
        except (OSError, ValueError):
            raise Failure('Approval API transport or JSON failed; write outcome may be unknown.') from None

    def remote(self, command, timeout=None):
        try:
            result = subprocess.run([
                'ssh', '-o', 'BatchMode=yes', '-o', 'ConnectTimeout=5',
                '-o', 'StrictHostKeyChecking=yes', '--', self.args.worker,
                shlex.join(command),
            ], capture_output=True, text=True, timeout=timeout or self.args.timeout)
        except (OSError, subprocess.TimeoutExpired):
            raise Failure('Worker SSH command failed or timed out.') from None
        return result

    def probe(self):
        # A cursor scopes denial evidence to this call, excluding previous rehearsals.
        cursor = self.remote(['sudo', '-n', 'journalctl', '-u', 'bramble-worker',
                              '-n', '0', '--show-cursor', '--no-pager', '-o', 'cat'])
        match = re.search(r'^-- cursor: (\S+)$', cursor.stdout, re.M)
        require(cursor.returncode == 0 and match, 'Cannot read a fresh worker journal cursor.')
        result = self.remote(['sudo', '-n', '-u', 'agent', 'curl', '-sS',
                              '--max-time', str(self.args.timeout), '-w', '\n%{http_code}',
                              self.args.forward], self.args.timeout + 10)
        body, _, status = result.stdout.rpartition('\n')
        if result.returncode == 0 and status == '200':
            try:
                healthy = json.loads(body).get('status') == 'healthy'
            except (ValueError, AttributeError):
                healthy = False
            require(healthy, 'Forward returned HTTP 200 without the expected healthy task JSON.')
            return 'allowed'
        require(result.returncode in (52, 56) and status == '000',
                'Forward failed without an explicit access denial; check transport and service health.')
        journal = self.remote(['sudo', '-n', 'journalctl', '-u', 'bramble-worker',
                               '--after-cursor', match[1], '--no-pager', '-o', 'cat'])
        require(journal.returncode == 0 and
                ('"' + self.args.service + '" denied by gateway') in journal.stdout,
                'Connection closed without a fresh gateway-denial journal line.')
        return 'denied'

    def payment(self, since):
        journal = self.remote(['sudo', '-n', 'journalctl', '-u', 'bramble-worker',
                               '--since', '@' + str(since), '--no-pager', '-o', 'cat'])
        require(journal.returncode == 0, 'Cannot read payment journal evidence.')
        match = re.search(r'https://hashscan\.io/testnet/transaction/0\.0\.\d+[@-]\d+[.\d-]*', journal.stdout)
        return match[0] if match else None


class OfflinePair:
    """In-memory rehearsal fixture. Never opens a socket or launches a process."""

    def __init__(self, _args):
        self.state = 'denied'

    def api(self, method, path, body=None):
        if method == 'POST' and path == '/api/requests':
            return {'id': 'offline-request', 'status': 'pending'}
        if method == 'POST' and path.endswith('/approve'):
            self.state = 'allowed'
            return {'status': 'approved', 'grantTransaction': {'transactionHash': '0x' + '1' * 64}}
        if method == 'POST' and path.endswith('/revoke'):
            self.state = 'denied'
            return {'status': 'revoked', 'revokeTransaction': {'transactionHash': '0x' + '2' * 64}}
        if method == 'GET' and path == '/api/audit':
            return {'entries': [{'requestId': 'offline-request', 'kind': 'grant',
                                 'status': 'posted', 'topicId': '0.0.123', 'sequenceNumber': '1'}]}
        raise Failure('Unexpected offline fixture operation.')

    def probe(self):
        return self.state

    def payment(self, _since):
        return 'https://hashscan.io/testnet/transaction/0.0.123@1.000000001'


class Rehearsal:
    def __init__(self, args, pair):
        self.args, self.pair = args, pair
        self.started = time.monotonic()
        self.failed = False

    def step(self, name, action):
        started = time.monotonic()
        print('RUN   ' + name, flush=True)
        try:
            detail = action()
        except (Failure, OSError, ValueError, KeyError, TypeError, AttributeError) as error:
            self.failed = True
            print(f'FAIL  {name} {time.monotonic() - started:.3f}s: ' +
                  (str(error) if isinstance(error, Failure) else 'Invalid response or transport failure.'), flush=True)
            raise Failure('Step failed.') from None
        print(f'PASS  {name} {time.monotonic() - started:.3f}s' +
              (': ' + str(detail) if isinstance(detail, str) and detail else ''), flush=True)

    def until(self, action, message):
        deadline = time.monotonic() + self.args.wait
        while True:
            found = action()
            if found:
                return found
            require(time.monotonic() < deadline, message)
            time.sleep(min(self.args.poll, max(0, deadline - time.monotonic())))

    def decision(self, path, expected, transaction, request_id):
        result = self.pair.api('POST', path)
        if result.get('status') in ('approving', 'revoking'):
            result = self.until(
                lambda: self.settled(request_id, expected), 'Transaction settlement timed out; do not resubmit.')
        require(result.get('status') == expected, 'Decision did not reach ' + expected + '.')
        tx = result.get(transaction, {}).get('transactionHash', '')
        require(re.fullmatch(r'0x[0-9a-fA-F]{64}', tx), 'Confirmed decision has no transaction hash.')
        return tx

    def settled(self, request_id, expected):
        result = self.pair.api('GET', '/api/requests/' + request_id)
        require(result.get('status') in ('approving', 'revoking', expected),
                'Transaction did not settle to the expected state.')
        return result if result.get('status') == expected else None

    def run(self):
        request_id = None
        grant_attempted = False
        revoke_attempted = False
        completed = False
        try:
            self.step('beat 1 denied', lambda: self.until(
                lambda: self.pair.probe() == 'denied', 'Initial denial timed out; reset the grant before retrying.'))
            def create():
                nonlocal request_id
                result = self.pair.api('POST', '/api/requests', {
                    'agentLabel': self.args.agent, 'gatewayLabel': self.args.gateway,
                    'service': self.args.service, 'note': 'ETH-287 demo rehearsal',
                })
                require(result.get('status') == 'pending', 'Request is not pending.')
                request_id = result.get('id', '')
                require(re.fullmatch(r'[A-Za-z0-9-]+', request_id), 'Request ID is missing or invalid.')
            self.step('beat 2 request', create)
            grant_attempted = True
            self.step('beat 2 approve', lambda: self.decision(
                '/api/requests/' + request_id + '/approve', 'approved', 'grantTransaction', request_id))
            call_since = int(time.time())
            self.step('beat 3 call', lambda: self.until(
                lambda: self.pair.probe() == 'allowed', 'Grant did not reach the gateway.'))
            # Evidence failures must still reach the revoke beat.
            try:
                self.step('beat 3 payment (rendezvous)', lambda: self.until(
                    lambda: self.pair.payment(call_since), 'No fresh rendezvous payment HashScan link.'))
                def audit():
                    rows = self.pair.api('GET', '/api/audit')['entries']
                    row = next((item for item in rows if item.get('requestId') == request_id
                                and item.get('kind') == 'grant'), None)
                    require(not row or row.get('status') != 'failed', 'Grant HCS audit failed; check audit configuration.')
                    if row and row.get('status') == 'posted':
                        topic, seq = row.get('topicId', ''), str(row.get('sequenceNumber', ''))
                        require(re.fullmatch(r'0\.0\.\d+', topic) and re.fullmatch(r'[1-9]\d*', seq),
                                'Posted audit has no valid topic or sequence number.')
                        return f'https://hashscan.io/testnet/topic/{topic} sequence={seq}'
                    return None
                self.step('beat 3 new HCS record', lambda: self.until(audit, 'Grant HCS audit timed out.'))
            except Failure:
                pass
            revoked_at = time.monotonic()
            revoke_attempted = True
            self.step('beat 5 revoke confirmed', lambda: self.decision(
                '/api/grants/' + self.args.agent + '/revoke', 'revoked', 'revokeTransaction', request_id))
            self.step('beat 5 denied after revoke', lambda: self.until(
                lambda: self.pair.probe() == 'denied', 'Revocation did not reach the gateway.'))
            print(f'TIMING revoke request to denied {time.monotonic() - revoked_at:.3f}s', flush=True)
            completed = True
        except Failure:
            pass
        finally:
            if not completed:
                self.failed = True
            if grant_attempted and not revoke_attempted:
                print('RUN   cleanup revoke after interrupted rehearsal', flush=True)
                try:
                    self.decision('/api/grants/' + self.args.agent + '/revoke',
                                  'revoked', 'revokeTransaction', request_id)
                    print('PASS  cleanup revoke confirmed', flush=True)
                except Failure:
                    self.failed = True
                    print('FAIL  cleanup revoke unresolved; operator must inspect grant state.', flush=True)
            print(f'{"FAIL" if self.failed else "PASS"}  rehearsal total {time.monotonic() - self.started:.3f}s', flush=True)
        return int(self.failed)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    mode = parser.add_mutually_exclusive_group(required=True)
    mode.add_argument('--offline', action='store_true', help='Simulate the sequence without network, SSH or signing')
    mode.add_argument('--base', type=origin, help='Approval API origin; executes grant and revoke on the prepared pair')
    parser.add_argument('--worker', default='bramble-worker', help='SSH alias of the existing worker')
    parser.add_argument('--agent', default='mumbai-worker')
    parser.add_argument('--gateway', default='mumbai-gateway')
    parser.add_argument('--service', default='task')
    parser.add_argument('--forward', default='http://127.0.0.1:8080/task')
    parser.add_argument('--timeout', type=positive, default=150, help='Per-call timeout in seconds')
    parser.add_argument('--wait', type=positive, default=180, help='Propagation/evidence wait in seconds; final call may use --timeout more')
    parser.add_argument('--poll', type=positive, default=2, help='Poll interval in seconds')
    args = parser.parse_args()
    for value in (args.agent, args.gateway, args.service):
        if not re.fullmatch(r'[a-z0-9][a-z0-9-]{0,62}', value):
            parser.error('Invalid agent, gateway or service label.')
    if not re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9._@-]*', args.worker):
        parser.error('Invalid worker SSH alias.')
    if not re.fullmatch(r'http://127\.0\.0\.1:\d{1,5}/[A-Za-z0-9/_-]*', args.forward):
        parser.error('Forward must be a worker loopback HTTP URL.')
    print('SIMULATED: offline fixtures; no live access, payment or chain verification.' if args.offline
          else 'LIVE: uses the prepared pair; node lifecycle remains with the ETH-244 operator.', flush=True)
    return Rehearsal(args, OfflinePair(args) if args.offline else Pair(args)).run()


if __name__ == '__main__':
    try:
        sys.exit(main())
    except Failure as error:
        print('FAIL  rehearsal: ' + str(error), file=sys.stderr)
        sys.exit(1)
