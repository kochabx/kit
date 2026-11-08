SHELL := /bin/bash
.SHELLFLAGS := -eu -o pipefail -c
.DEFAULT_GOAL := help

GO           ?= go
PKGS         ?= ./...
GOFLAGS      ?= -buildvcs=false
TEST_TIMEOUT ?= 120s
export GOFLAGS

MODULE  := $(shell $(GO) list -m 2>/dev/null)
PROJECT ?= $(notdir $(MODULE))

ifeq ($(NO_COLOR),)
GREEN  := \033[32m
YELLOW := \033[33m
BLUE   := \033[34m
RESET  := \033[0m
else
GREEN  :=
YELLOW :=
BLUE   :=
RESET  :=
endif

.PHONY: help info fmt fmt-check tidy deps deps-upgrade vet test check build

help: ## Show available targets
	@printf '$(GREEN)%s 开发命令$(RESET)\n\n$(YELLOW)可用命令:$(RESET)\n' '$(PROJECT)'
	@awk 'BEGIN {FS = ":.*## "} \
		/^[a-zA-Z0-9_.-]+:.*## / {printf "  $(BLUE)%-18s$(RESET) %s\n", $$1, $$2}' \
		$(MAKEFILE_LIST) | sort

info: ## Show project configuration
	@printf '$(BLUE)项目信息$(RESET)\n'
	@printf '  %-10s %s\n' \
		project '$(PROJECT)' module '$(MODULE)' go "$$($(GO) version)"

fmt: ## Format Go source
	@printf '$(BLUE)正在格式化 Go 源码...$(RESET)\n'
	@$(GO) fmt $(PKGS)
	@printf '$(GREEN)格式化完成$(RESET)\n'

fmt-check: ## Check Go source formatting
	@printf '$(BLUE)正在检查 Go 源码格式...$(RESET)\n'
	@files="$$(find . -type f -name '*.go' -not -path './vendor/*' -exec gofmt -l {} +)"; \
		if [[ -n "$$files" ]]; then \
			printf '$(YELLOW)以下文件尚未格式化:\n%s$(RESET)\n' "$$files"; \
			exit 1; \
		fi
	@printf '$(GREEN)格式检查通过$(RESET)\n'

tidy: ## Tidy go.mod and go.sum
	@printf '$(BLUE)正在整理模块依赖...$(RESET)\n'
	@$(GO) mod tidy
	@printf '$(GREEN)模块依赖整理完成$(RESET)\n'

deps: ## Download module dependencies
	@printf '$(BLUE)正在下载模块依赖...$(RESET)\n'
	@$(GO) mod download
	@printf '$(GREEN)模块依赖下载完成$(RESET)\n'

deps-upgrade: ## Upgrade dependencies and tidy modules
	@printf '$(BLUE)正在升级模块依赖...$(RESET)\n'
	@$(GO) get -u $(PKGS)
	@$(GO) mod tidy
	@printf '$(GREEN)模块依赖升级完成$(RESET)\n'

vet: ## Run Go static analysis
	@printf '$(BLUE)正在执行静态检查...$(RESET)\n'
	@$(GO) vet $(PKGS)
	@printf '$(GREEN)静态检查通过$(RESET)\n'

test: ## Run tests with the race detector
	@printf '$(BLUE)正在执行测试和竞态检测...$(RESET)\n'
	@$(GO) test -race -timeout $(TEST_TIMEOUT) $(PKGS)
	@printf '$(GREEN)测试通过$(RESET)\n'

check: fmt-check vet test ## Run all local checks

build: ## Build all Go packages
	@printf '$(BLUE)正在编译 %s...$(RESET)\n' '$(PROJECT)'
	@$(GO) build $(PKGS)
	@printf '$(GREEN)编译通过$(RESET)\n'
