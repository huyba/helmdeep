# Kubernetes manifests

Plain manifests, not a Helm chart — deliberately, for a single-service demo
deployment. `configmap.yaml`, `secret.yaml`, `deployment.yaml`,
`service.yaml`.

## Try it locally (kind/minikube)

```sh
# from the repo root
docker build -f deploy/docker/Dockerfile -t helmdeep-gateway:latest .

# kind:
kind load docker-image helmdeep-gateway:latest
# minikube:
minikube image load helmdeep-gateway:latest

kubectl apply -f deploy/k8s/configmap.yaml
kubectl apply -f deploy/k8s/secret.yaml   # example values — see the file's own warning
kubectl apply -f deploy/k8s/deployment.yaml
kubectl apply -f deploy/k8s/service.yaml

kubectl rollout status deployment/helmdeep-gateway
kubectl port-forward svc/helmdeep-gateway 8443:8443
```

Then the same `curl` commands as `examples/quickstart/README.md` work
against `localhost:8443`.

## Known limitations — read before using this beyond a demo

- **`replicas: 1` is load-bearing, not a default left unconsidered.**
  `internal/gateway`'s rate limiter and provenance cache are in-process
  state with no cross-pod coordination. Scaling this Deployment out today
  silently changes what "3 calls per minute" means (it becomes 3 calls per
  minute *per pod that happens to answer*) and breaks provenance
  correlation across pods. Don't raise `replicas` without addressing that
  first.
- **The audit log lives in an `emptyDir`.** It does not survive a pod
  restart or reschedule. Replace with a `PersistentVolumeClaim` — and
  decide what "durable" means for your audit requirement — before this
  deployment holds anything you'd need to produce as evidence later.
- **No dedicated health endpoint.** The probes are TCP-socket checks
  against the MCP port, which prove the process is up, not that policy
  loaded correctly or the audit log is writable. A real `/healthz` would
  be a meaningfully better signal; it doesn't exist yet.
- **`secret.yaml` ships placeholder values.** Read its own comments before
  applying it anywhere real.
- **The image tag `latest` in `deployment.yaml` is a placeholder.** Point
  it at your registry and a real tag (a git SHA or release tag) before
  this leaves a local demo — see `.github/workflows/release.yml` for how
  this repo's own multi-arch image build works.
