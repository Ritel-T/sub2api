#!/bin/sh
set -eu
repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
cd "$repo_root"

python3 - <<'PY'
import json
import os
import subprocess

# Render our templates. Do not duplicate Docker's environment precedence matrix
# or assert indentation/spelling in YAML source.
for filename in ("docker-compose.yml", "docker-compose.local.yml",
                 "docker-compose.standalone.yml", "docker-compose.dev.yml"):
    for overrides in ({}, {"SIMPLE_MODE_AUTO_CREATE_DEFAULT_GROUPS": "false",
                           "SERVER_TRUSTED_PROXIES": "127.0.0.1/32,::1/128",
                           "GATEWAY_MAX_CONNS_PER_HOST": "64"},
                      {"SERVER_TRUSTED_PROXIES": ""}):
        env = dict(os.environ, POSTGRES_PASSWORD="compose-test-password",
                   DATABASE_HOST="postgres", DATABASE_PASSWORD="compose-test-password",
                   REDIS_HOST="redis")
        for key in ("SIMPLE_MODE_AUTO_CREATE_DEFAULT_GROUPS", "SERVER_TRUSTED_PROXIES",
                    "GATEWAY_MAX_CONNS_PER_HOST", "GATEWAY_MAX_IDLE_CONNS",
                    "GATEWAY_MAX_IDLE_CONNS_PER_HOST"):
            env.pop(key, None)
        env.update(overrides)
        result = subprocess.run(["docker", "compose", "--env-file", "/dev/null", "-f",
                                 "deploy/" + filename, "config", "--format", "json"],
                                env=env, capture_output=True, text=True, check=True)
        app = json.loads(result.stdout)["services"]["sub2api"]
        actual = app["environment"]
        assert "no-new-privileges:true" in app["security_opt"], filename
        assert actual["SIMPLE_MODE_AUTO_CREATE_DEFAULT_GROUPS"] == overrides.get("SIMPLE_MODE_AUTO_CREATE_DEFAULT_GROUPS", ""), filename
        assert actual.get("SERVER_TRUSTED_PROXIES") == overrides.get("SERVER_TRUSTED_PROXIES"), filename
        assert actual["GATEWAY_MAX_CONNS_PER_HOST"] == overrides.get("GATEWAY_MAX_CONNS_PER_HOST", "1024"), filename
        assert actual["GATEWAY_MAX_IDLE_CONNS"] == "2560", filename
        assert actual["GATEWAY_MAX_IDLE_CONNS_PER_HOST"] == "120", filename
print("Compose security and environment behavior passed (four templates, three distinct cases)")
PY
