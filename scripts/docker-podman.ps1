# Run Docker CLI / Docker Compose commands against the Podman engine.
# Podman machine forwards a Docker-compatible API on this pipe.
# Usage: .\scripts\docker-podman.ps1 <docker args...>
#   e.g. .\scripts\docker-podman.ps1 compose up -d
$env:DOCKER_HOST = 'npipe:////./pipe/docker_engine'
& docker @args
