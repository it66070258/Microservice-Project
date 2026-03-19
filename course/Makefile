# Makefile for Course Service

.PHONY: help build test run docker-up docker-down docker-logs docker-test clean

help: ## Show this help message
	@echo "Available commands:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2}'

build: ## Build the Go application
	go build -o course.exe .

test: ## Run Go tests
	go test -v -timeout 30s

run: ## Run the application locally
	go run .

docker-build: ## Build Docker image
	docker compose build

docker-up: ## Start all Docker containers
	docker compose up -d
	@echo "Waiting for services to be ready..."
	@sleep 5
	@echo "Services are up! Course service: http://localhost:8000"

docker-down: ## Stop all Docker containers
	docker compose down

docker-logs: ## Show Docker logs
	docker compose logs -f

docker-restart: ## Restart Docker containers
	docker compose restart

docker-clean: ## Stop and remove all containers, volumes, and images
	docker compose down -v
	docker system prune -f

docker-test: ## Run API tests against Docker containers
	@echo "Running API tests..."
	@bash test-api.sh || powershell -File test-api.ps1

migrate: ## Run database migrations
	PGPASSWORD=1234 psql -U postgres -h localhost -d register -f migrations/create_metrics_table.sql

seed: ## Seed database with sample data
	@echo "Seeding database..."
	@go run seed.go

clean: ## Clean build artifacts
	rm -f course.exe
	rm -f course-service

status: ## Check Docker container status
	docker compose ps

all: build docker-up docker-test ## Build, start Docker, and run tests
