.PHONY: build build-backend build-frontend build-ritel test test-backend test-frontend

# 一键编译前后端
build: build-frontend
	@$(MAKE) -C backend build-embedded

# Production artifact + embedded frontend manifest, built on the local machine.
# BUILD_ARGS may supply --output, --go, --version or --offline.
build-ritel:
	@python3 deploy/build-local.py --ritel $(BUILD_ARGS)

# 编译后端（复用 backend/Makefile）
build-backend:
	@$(MAKE) -C backend build

# 编译前端（需要已安装依赖）
build-frontend:
	@pnpm --dir frontend run build --outDir ../backend/internal/web/dist

# 运行测试（后端 + 前端）
test: test-backend test-frontend

test-backend:
	@$(MAKE) -C backend test

test-frontend:
	@pnpm --dir frontend run lint:check
	@pnpm --dir frontend run typecheck
	@pnpm --dir frontend run test:run
