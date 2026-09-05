.PHONY: dev dev-debug test web-check gateway-check

dev:
	docker compose --env-file deploy/.env -f deploy/docker-compose.yaml up --build

dev-debug:
	docker compose --env-file deploy/.env -f deploy/docker-compose.yaml -f deploy/docker-compose.debug.yaml up --build

gateway-check:
	docker compose -p lester-gateway-test -f deploy/gateway/compose.test.yaml up --abort-on-container-exit --exit-code-from checks
	docker compose -p lester-gateway-test -f deploy/gateway/compose.test.yaml down

test:
	cd backend && go test ./...

web-check:
	pnpm --dir frontend lint
	pnpm --dir frontend build
