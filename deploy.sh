#!/usr/bin/env bash
# deploy.sh — Sub2API build-and-deploy pipeline
# Run from the repo root on oc-dev (C).
#
# Usage:
#   ./deploy.sh build          Build image on C, tag with git short hash
#   ./deploy.sh deploy-c       Restart C with the latest build
#   ./deploy.sh push   <a|b>   Push image to target and recreate service
#   ./deploy.sh push   ab      Push to both A and B
#   ./deploy.sh status         Show image/container state on all instances
#   ./deploy.sh rollback <a|b> <tag>   Roll back target to a previous tag
#
# Tags: each build produces sub2api:opusclaw-<git-hash> (immutable).
# Alias sub2api:opusclaw is the single stable tag all compose files reference.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")" && pwd)"
COMPOSE_DIR="/srv/sub2api-c"
IMAGE_BASE="sub2api"
ALIAS_TAG="opusclaw"

declare -A HOSTS=( [a]="100.114.245.91" [b]="100.112.136.98" )
declare -A COMPOSE_FILES=( [a]="/srv/sub2api/docker-compose.yml" [b]="/srv/sub2api/docker-compose.yml" )
declare -A COMPOSE_PROJECTS=( [a]="sub2api" [b]="sub2api" )

version_tag() {
  local short_hash
  short_hash=$(git -C "$REPO_ROOT" rev-parse --short HEAD 2>/dev/null || echo "unknown")
  echo "opusclaw-${short_hash}"
}

log()  { printf '\033[1;34m==>\033[0m %s\n' "$*"; }
ok()   { printf '\033[1;32m  ✓\033[0m %s\n' "$*"; }
fail() { printf '\033[1;31m  ✗\033[0m %s\n' "$*"; exit 1; }
warn() { printf '\033[1;33m  !\033[0m %s\n' "$*"; }

cmd_build() {
  local tag
  tag=$(version_tag)
  log "Building ${IMAGE_BASE}:${tag} ..."

  cd "$COMPOSE_DIR"
  SUB2API_TAG="${tag}" docker compose build --no-cache

  docker tag "${IMAGE_BASE}:${tag}" "${IMAGE_BASE}:${ALIAS_TAG}"

  ok "Built: ${IMAGE_BASE}:${tag}"
  ok "Alias: ${ALIAS_TAG}"
  echo
  docker images --format 'table {{.Repository}}:{{.Tag}}\t{{.ID}}\t{{.CreatedAt}}' \
    | grep "^${IMAGE_BASE}:" | head -10 || true
}

cmd_deploy_c() {
  log "Deploying to C (local) ..."
  cd "$COMPOSE_DIR"
  docker compose up -d sub2api
  sleep 8
  local health
  health=$(curl -sf http://localhost:8000/health 2>/dev/null || echo '{"status":"error"}')
  if echo "$health" | grep -q '"ok"'; then
    ok "C healthy: ${health}"
  else
    fail "C health check failed: ${health}"
  fi
}

cmd_push() {
  local targets="$1"
  local tag
  tag=$(version_tag)

  if ! docker image inspect "${IMAGE_BASE}:${tag}" >/dev/null 2>&1; then
    fail "Image ${IMAGE_BASE}:${tag} not found. Run './deploy.sh build' first."
  fi

  local push_failures=0
  for t in $(echo "$targets" | grep -o .); do
    local ip="${HOSTS[$t]:-}"
    local compose_file="${COMPOSE_FILES[$t]:-}"
    local project="${COMPOSE_PROJECTS[$t]:-}"
    [[ -z "$ip" ]] && { fail "Unknown target: $t (valid: a, b)"; }

    log "Pushing ${IMAGE_BASE}:${tag} → ${t^^} (${ip}) ..."

    if ! docker save "${IMAGE_BASE}:${tag}" | gzip | ssh "root@${ip}" "gunzip | docker load"; then
      warn "${t^^}: image transfer failed"
      push_failures=$((push_failures + 1))
      continue
    fi
    ssh "root@${ip}" "docker tag ${IMAGE_BASE}:${tag} ${IMAGE_BASE}:${ALIAS_TAG}"
    ok "Image loaded and aliased on ${t^^}"

    log "Recreating service on ${t^^} ..."
    ssh "root@${ip}" "docker compose -f ${compose_file} -p ${project} up -d sub2api"

    log "Waiting for health check on ${t^^} ..."
    local health
    health=$(ssh "root@${ip}" "sleep 10 && curl -sf http://localhost:8000/health" 2>/dev/null || echo '{"status":"error"}')
    if echo "$health" | grep -q '"ok"'; then
      ok "${t^^} healthy: ${health}"
    else
      warn "${t^^} health check failed: ${health}"
      push_failures=$((push_failures + 1))
    fi
    echo
  done

  [[ $push_failures -gt 0 ]] && fail "${push_failures} target(s) failed"
}

cmd_status() {
  log "Local images:"
  docker images --format 'table {{.Repository}}:{{.Tag}}\t{{.ID}}\t{{.CreatedAt}}' \
    | { grep "^${IMAGE_BASE}:" || true; } | head -10
  echo

  log "C container:"
  docker ps --format 'table {{.Names}}\t{{.Image}}\t{{.Status}}' \
    | { grep '^sub2api-c ' || echo "  (not running)"; }
  echo

  for t in a b; do
    local ip="${HOSTS[$t]}"
    log "${t^^} (${ip}):"
    ssh "root@${ip}" "docker ps --format 'table {{.Names}}\t{{.Image}}\t{{.Status}}' | grep '^sub2api-' || echo '  (not running)'" 2>/dev/null || echo "  (unreachable)"
    ssh "root@${ip}" "docker images --format 'table {{.Repository}}:{{.Tag}}\t{{.ID}}\t{{.CreatedAt}}' | grep '^sub2api:' | head -5 || true" 2>/dev/null || echo "  (unreachable)"
    echo
  done
}

cmd_rollback() {
  local target="$1"
  local tag="$2"
  local ip="${HOSTS[$target]:-}"
  local compose_file="${COMPOSE_FILES[$target]:-}"
  local project="${COMPOSE_PROJECTS[$target]:-}"
  [[ -z "$ip" ]] && fail "Unknown target: $target"

  log "Rolling back ${target^^} to ${IMAGE_BASE}:${tag} ..."

  ssh "root@${ip}" "docker tag ${IMAGE_BASE}:${tag} ${IMAGE_BASE}:${ALIAS_TAG}"
  ssh "root@${ip}" "docker compose -f ${compose_file} -p ${project} up -d sub2api"

  local health
  health=$(ssh "root@${ip}" "sleep 10 && curl -sf http://localhost:8000/health" 2>/dev/null || echo '{"status":"error"}')
  if echo "$health" | grep -q '"ok"'; then
    ok "${target^^} rolled back and healthy"
  else
    fail "${target^^} health check failed after rollback"
  fi
}

case "${1:-help}" in
  build)
    cmd_build
    ;;
  deploy-c)
    cmd_deploy_c
    ;;
  push)
    [[ -z "${2:-}" ]] && fail "Usage: deploy.sh push <a|b|ab>"
    cmd_push "$2"
    ;;
  status)
    cmd_status
    ;;
  rollback)
    [[ -z "${2:-}" || -z "${3:-}" ]] && fail "Usage: deploy.sh rollback <a|b> <tag>"
    cmd_rollback "$2" "$3"
    ;;
  *)
    echo "Usage: deploy.sh <build|deploy-c|push|status|rollback> [args]"
    echo
    echo "Commands:"
    echo "  build                Build image on C, tag with git commit hash"
    echo "  deploy-c             Restart C with current build"
    echo "  push <a|b|ab>        Push image to target(s) and recreate"
    echo "  status               Show image/container state on all instances"
    echo "  rollback <a|b> <tag> Roll back to a previous image tag"
    echo
    echo "Typical workflow:"
    echo "  ./deploy.sh build          # Build on C"
    echo "  ./deploy.sh deploy-c       # Test on C"
    echo "  ./deploy.sh push a         # Push to A"
    echo "  ./deploy.sh push b         # Push to B"
    echo "  ./deploy.sh push ab        # Push to both"
    echo "  ./deploy.sh status         # Check everything"
    exit 1
    ;;
esac
