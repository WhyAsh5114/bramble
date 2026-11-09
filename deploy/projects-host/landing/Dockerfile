# The landing page plus the install scripts, served by unprivileged nginx.
# Build context: the repository root.
FROM nginxinc/nginx-unprivileged:1.27-alpine
COPY deploy/projects-host/landing/nginx.conf /etc/nginx/conf.d/default.conf
COPY landing/index.html landing/styles.css /usr/share/nginx/html/
COPY landing/assets /usr/share/nginx/html/assets
COPY scripts/release/install.sh scripts/release/uninstall.sh /usr/share/nginx/html/
