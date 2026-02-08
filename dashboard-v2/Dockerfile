FROM node:22-alpine AS build
WORKDIR /app

COPY package*.json ./
RUN npm ci

COPY . .

RUN npm run build

FROM nginx:1.27-alpine
COPY --from=build /app/dist /usr/share/nginx/html
COPY nginx/default.conf.template /etc/nginx/templates/default.conf.template

# Runtime upstream for /api/* reverse proxy (set per environment).
ENV API_UPSTREAM=http://host.docker.internal:8080

EXPOSE 80

CMD ["nginx", "-g", "daemon off;"]
