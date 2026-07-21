FROM nginx:1.29-alpine

COPY internal/orchestrator/web/index.html /usr/share/nginx/html/index.html
COPY nginx.conf /etc/nginx/conf.d/default.conf
