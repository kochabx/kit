# Go Kit Makefile - 微服务工具包构建脚本

.PHONY: all build test coverage lint clean install upgrade proto wire swag fmt vet mod-tidy security help

# 项目变量
PKG := github.com/kochabx/kit
GOPATH := $(shell go env GOPATH)
GOBIN := $(GOPATH)/bin
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME := $(shell date -u '+%Y-%m-%d_%H:%M:%S')
LDFLAGS := -X '$(PKG)/version.Version=$(VERSION)' -X '$(PKG)/version.BuildTime=$(BUILD_TIME)'

# 颜色输出
GREEN := \033[32m
RED := \033[31m
YELLOW := \033[33m
BLUE := \033[34m
NC := \033[0m # No Color

## Code Quality
fmt: ## 格式化代码
	@echo "$(BLUE)格式化代码...$(NC)"
	@go fmt ./...
	@echo "$(GREEN)✓ 代码格式化完成$(NC)"

vet: ## 静态代码分析
	@echo "$(BLUE)静态代码分析...$(NC)"
	@go vet ./...
	@echo "$(GREEN)✓ 静态分析完成$(NC)"

## Dependencies
install: ## 安装开发依赖工具
	@echo "$(BLUE)安装开发工具...$(NC)"
	@go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
	@go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
	@go install github.com/favadi/protoc-go-inject-tag@latest
	@go install github.com/google/wire/cmd/wire@latest
	@go install github.com/swaggo/swag/cmd/swag@latest
	@echo "$(GREEN)✓ 开发工具安装完成$(NC)"

upgrade: ## 升级项目依赖
	@echo "$(BLUE)升级依赖包...$(NC)"
	@go get -u ./...
	@go mod tidy
	@echo "$(GREEN)✓ 依赖升级完成$(NC)"

## Code Generation
proto: ## 生成 gRPC 代码
	@if find . -name "*.proto" -type f -print -quit | grep -q .; then \
		echo "$(BLUE)生成 gRPC 代码...$(NC)"; \
		protoc -I=. -I=../.. --go_out=. --go_opt=module=${PKG} --go-grpc_out=. --go-grpc_opt=module=${PKG} */*.proto && \
		protoc-go-inject-tag -input="*/*.pb.go" && \
		go fmt ./... && \
		echo "$(GREEN)✓ gRPC 代码生成完成$(NC)"; \
	else \
		echo "$(YELLOW)⚠ 未发现 .proto 文件，跳过代码生成$(NC)"; \
	fi

wire: ## 生成 Wire 依赖注入代码
	@if find . -name "wire.go" -type f -print -quit | grep -q .; then \
		echo "$(BLUE)生成 Wire 代码...$(NC)"; \
		wire ./... && echo "$(GREEN)✓ Wire 代码生成完成$(NC)"; \
	else \
		echo "$(YELLOW)⚠ 未发现 wire.go 文件，跳过代码生成$(NC)"; \
	fi

swag: ## 生成 Swagger 文档
	@if find . -name "main.go" -type f -print -quit | grep -q .; then \
		echo "$(BLUE)生成 Swagger 文档...$(NC)"; \
		swag init && echo "$(GREEN)✓ Swagger 文档生成完成$(NC)"; \
	else \
		echo "$(YELLOW)⚠ 未发现 main.go 文件，跳过 Swagger 文档生成$(NC)"; \
	fi

generate: proto wire swag ## 生成所有代码

## Information
info: ## 显示项目信息
	@echo "$(BLUE)项目信息:$(NC)"
	@echo "  包名: $(PKG)"
	@echo "  版本: $(VERSION)"
	@echo "  构建时间: $(BUILD_TIME)"
	@echo "  Go版本: $(shell go version)"
	@echo "  GOPATH: $(GOPATH)"
	@echo "  GOBIN: $(GOBIN)"

help: ## 显示帮助信息
	@echo "$(GREEN)Go Kit Makefile 使用指南$(NC)"
	@echo
	@echo "$(YELLOW)基本命令:$(NC)"
	@grep -h -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  $(BLUE)%-20s$(NC) %s\n", $$1, $$2}' | \
		sort

.DEFAULT_GOAL := help