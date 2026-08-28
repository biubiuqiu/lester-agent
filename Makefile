.PHONY: dev test web-check

dev:
	docker compose --env-file deploy/.env -f deploy/docker-compose.yaml up --build

test:
	cd backend && go test ./...

web-check:
	pnpm --dir frontend lint
	pnpm --dir frontend build
