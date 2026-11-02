.PHONY: test test-backend test-frontend lint build up down

up:
	docker compose up --build

down:
	docker compose down -v

test: test-backend test-frontend

test-backend:
	cd backend && go vet ./... && go test -race -p 1 ./...

test-frontend:
	cd frontend && npm run lint && npm run typecheck && npm run test && npm run build
