FROM node:22-alpine AS build
WORKDIR /src
COPY package.json package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY . .
ARG DEMO_MODE=false
ARG DEMO_PASSWORD
RUN if [ "$DEMO_MODE" = true ]; then \
      export VITE_DEMO_ENABLED=true VITE_DEMO_PASSWORD="$DEMO_PASSWORD"; \
    else \
      export VITE_DEMO_ENABLED=false VITE_DEMO_PASSWORD=; \
    fi; npm run build

FROM nginxinc/nginx-unprivileged:1.27-alpine
COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /src/dist /usr/share/nginx/html
USER 101
EXPOSE 8080
