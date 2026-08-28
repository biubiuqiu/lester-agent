.PHONY: dev test web-check

dev:
	docker compose --env-file deploy/docker-compose/.env -f deploy/docker-compose/compose.yaml up --build

test:
	go test ./...

web-check:
	pnpm --dir apps/web lint
	pnpm --dir apps/web build

