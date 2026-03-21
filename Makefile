# Go Kit Makefile - modernized build and verification workflow

SHELL := /bin/bash
.SHELLFLAGS := --noprofile --norc -eu -o pipefail -c
MAKEFLAGS += --warn-undefined-variables --no-builtin-rules

unexport BASH_ENV
unexport ENV

GO ?= go
MODULE := github.com/kochabx/kit
GOPATH := $(shell $(GO) env GOPATH)
GOBIN ?= $(GOPATH)/bin
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME := $(shell date -u '+%Y-%m-%d_%H:%M:%S')
LDFLAGS := -X '$(MODULE)/version.Version=$(VERSION)' -X '$(MODULE)/version.BuildTime=$(BUILD_TIME)'

WIRE_BIN := $(GOBIN)/wire
SWAG_BIN := $(GOBIN)/swag
PROTOC_GEN_GO_BIN := $(GOBIN)/protoc-gen-go
PROTOC_GEN_GO_GRPC_BIN := $(GOBIN)/protoc-gen-go-grpc
PROTOC_GO_INJECT_TAG_BIN := $(GOBIN)/protoc-go-inject-tag

TEST_TIMEOUT ?= 120s

GREEN := \033[32m
RED := \033[31m
YELLOW := \033[33m
BLUE := \033[34m
BOLD := \033[1m
NC := \033[0m

.PHONY: all build test clean install upgrade proto wire swag fmt vet mod-tidy generate info help

.DEFAULT_GOAL := help

all: fmt vet test ## 完整本地校验

##@ 构建
build: ## 编译所有包
	@printf "$(BLUE)编译验证...$(NC)\n"
	@$(GO) build ./...
	@printf "$(GREEN)✓ 编译通过$(NC)\n"

##@ 测试
test: ## 全量测试（含 race）
	@printf "$(BLUE)运行测试...$(NC)\n"
	@$(GO) test -race -timeout $(TEST_TIMEOUT) ./...
	@printf "$(GREEN)✓ 测试通过$(NC)\n"

##@ 代码质量
fmt: ## 使用 gofmt 简化并格式化代码
	@printf "$(BLUE)格式化代码...$(NC)\n"
	@find . -type f -name '*.go' -not -path './vendor/*' -print0 | xargs -0 gofmt -w -s
	@printf "$(GREEN)✓ 代码格式化完成$(NC)\n"

vet: ## 使用 go vet 做静态检查
	@printf "$(BLUE)静态代码分析...$(NC)\n"
	@$(GO) vet ./...
	@printf "$(GREEN)✓ 静态分析完成$(NC)\n"

##@ 依赖
mod-tidy: ## 整理并校验 Go 模块依赖
	@printf "$(BLUE)整理依赖...$(NC)\n"
	@$(GO) mod tidy
	@$(GO) mod verify
	@printf "$(GREEN)✓ 依赖整理完成$(NC)\n"

install: $(PROTOC_GEN_GO_BIN) $(PROTOC_GEN_GO_GRPC_BIN) $(PROTOC_GO_INJECT_TAG_BIN) $(WIRE_BIN) $(SWAG_BIN) ## 安装开发工具
	@printf "$(GREEN)✓ 开发工具安装完成$(NC)\n"

upgrade: ## 升级依赖并整理模块
	@printf "$(BLUE)升级依赖包...$(NC)\n"
	@$(GO) get -u ./...
	@$(MAKE) mod-tidy
	@printf "$(GREEN)✓ 依赖升级完成$(NC)\n"

$(GOBIN):
	@mkdir -p $@

$(PROTOC_GEN_GO_BIN): | $(GOBIN)
	@printf "$(BLUE)安装 protoc-gen-go...$(NC)\n"
	@GOBIN=$(GOBIN) $(GO) install google.golang.org/protobuf/cmd/protoc-gen-go@latest

$(PROTOC_GEN_GO_GRPC_BIN): | $(GOBIN)
	@printf "$(BLUE)安装 protoc-gen-go-grpc...$(NC)\n"
	@GOBIN=$(GOBIN) $(GO) install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest

$(PROTOC_GO_INJECT_TAG_BIN): | $(GOBIN)
	@printf "$(BLUE)安装 protoc-go-inject-tag...$(NC)\n"
	@GOBIN=$(GOBIN) $(GO) install github.com/favadi/protoc-go-inject-tag@latest

$(WIRE_BIN): | $(GOBIN)
	@printf "$(BLUE)安装 wire...$(NC)\n"
	@GOBIN=$(GOBIN) $(GO) install github.com/google/wire/cmd/wire@latest

$(SWAG_BIN): | $(GOBIN)
	@printf "$(BLUE)安装 swag...$(NC)\n"
	@GOBIN=$(GOBIN) $(GO) install github.com/swaggo/swag/cmd/swag@latest

##@ 代码生成
proto: $(PROTOC_GEN_GO_BIN) $(PROTOC_GEN_GO_GRPC_BIN) $(PROTOC_GO_INJECT_TAG_BIN) ## 生成 gRPC 代码
	@if find . -name '*.proto' -type f -print -quit | grep -q .; then \
		printf "$(BLUE)生成 gRPC 代码...$(NC)\n"; \
		PATH='$(GOBIN):'"$$PATH" protoc -I=. -I=../.. \
			--go_out=. --go_opt=module=$(MODULE) \
			--go-grpc_out=. --go-grpc_opt=module=$(MODULE) \
			$$(find . -name '*.proto' -type f) && \
		PATH='$(GOBIN):'"$$PATH" $(PROTOC_GO_INJECT_TAG_BIN) -input="$$(find . -name '*.pb.go' -type f)" && \
		$(GO) fmt ./... && \
		printf "$(GREEN)✓ gRPC 代码生成完成$(NC)\n"; \
	else \
		printf "$(YELLOW)未发现 .proto 文件，跳过代码生成$(NC)\n"; \
	fi

wire: $(WIRE_BIN) ## 生成 Wire 依赖注入代码
	@if find . -name 'wire.go' -type f -print -quit | grep -q .; then \
		printf "$(BLUE)生成 Wire 代码...$(NC)\n"; \
		PATH='$(GOBIN):'"$$PATH" $(WIRE_BIN) ./... && \
		printf "$(GREEN)✓ Wire 代码生成完成$(NC)\n"; \
	else \
		printf "$(YELLOW)未发现 wire.go 文件，跳过代码生成$(NC)\n"; \
	fi

swag: $(SWAG_BIN) ## 生成 Swagger 文档
	@if find . -name 'main.go' -type f -print -quit | grep -q .; then \
		printf "$(BLUE)生成 Swagger 文档...$(NC)\n"; \
		PATH='$(GOBIN):'"$$PATH" $(SWAG_BIN) init && \
		printf "$(GREEN)✓ Swagger 文档生成完成$(NC)\n"; \
	else \
		printf "$(YELLOW)未发现 main.go 文件，跳过 Swagger 文档生成$(NC)\n"; \
	fi

generate: proto wire swag ## 生成所有代码

##@ 清理
clean: ## 清理覆盖率等生成物
	@printf "$(BLUE)清理生成文件...$(NC)\n"
	@rm -rf $(COVERAGE_DIR)
	@printf "$(GREEN)✓ 清理完成$(NC)\n"

##@ 信息
info: ## 显示项目信息
	@printf "$(BLUE)项目信息:$(NC)\n"
	@printf "  模块: %s\n" "$(MODULE)"
	@printf "  版本: %s\n" "$(VERSION)"
	@printf "  构建时间: %s\n" "$(BUILD_TIME)"
	@printf "  Go版本: %s\n" "$$( $(GO) version )"
	@printf "  GOPATH: %s\n" "$(GOPATH)"
	@printf "  GOBIN: %s\n" "$(GOBIN)"

##@ 帮助
help: ## 显示帮助信息
	@printf "$(BOLD)$(GREEN)Go Kit Makefile 使用指南$(NC)\n"
	@awk 'BEGIN {FS = ":.*## "} \
		/^[a-zA-Z0-9_./-]+:.*## / {printf "  $(BLUE)%-18s$(NC) %s\n", $$1, $$2}' $(MAKEFILE_LIST)