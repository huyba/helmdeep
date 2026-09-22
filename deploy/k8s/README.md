# Kubernetes manifests

Plain manifests, not a Helm chart — deliberately, for a single-service demo
deployment. `configmap.yaml`, `secret.yaml`, `pvc.yaml`, `deployment.yaml`,
`service.yaml`.

## Try it locally (kind/minikube)

```sh
# from the repo root
docker build -f deploy/docker/Dockerfile -t helmdeep-tool-gateway:latest .

# kind:
kind load docker-image helmdeep-tool-gateway:latest
# minikube:
minikube image load helmdeep-tool-gateway:latest

kubectl apply -f deploy/k8s/configmap.yaml
kubectl apply -f deploy/k8s/secret.yaml   # example values — see the file's own warning
kubectl apply -f deploy/k8s/pvc.yaml      # audit log volume — uses the cluster's default StorageClass
kubectl apply -f deploy/k8s/deployment.yaml
kubectl apply -f deploy/k8s/service.yaml

kubectl rollout status deployment/helmdeep-tool-gateway
kubectl port-forward svc/helmdeep-tool-gateway 8443:8443
```

Then the same `curl` commands as `examples/quickstart/README.md` work
against `localhost:8443`.

## Continuous deployment to Azure Kubernetes Service (AKS)

`.github/workflows/deploy.yml` builds and pushes an image to GHCR
(`ghcr.io/huyba/helmdeep`) and rolls it out to a real AKS cluster on every
push to `main`. This is dev/demo infrastructure — see "Known limitations"
below before treating it as production.

**The cluster:** resource group `helmdeep-rg`, cluster `helmdeep-aks`,
region `westus2` (chosen for latency from the primary maintainer's
location; the subscription's quota in this account only allows D/E/F/M/H-
series VMs, no burstable B-series, so the node pool runs a single
`Standard_D2as_v7` rather than something smaller — there was no cheaper
SKU available at all). Free-tier control plane (no SLA, fine for a dev
cluster). `az aks list -o table` confirms what actually exists; this file
describes intent; `az` is the source of truth if they ever disagree.

**One-time setup this repo's CI can't do for itself** (needs a human with
GitHub repo-admin and Azure subscription access):

1. **GHCR package visibility.** After `deploy.yml` runs once and pushes an
   image, go to the package's settings on GitHub
   (github.com/huyba/helmdeep → Packages → helmdeep → Package settings)
   and set visibility to **Public**. Chosen over a private package +
   `imagePullSecret` for simplicity — no Kubernetes-side pull credential
   to manage at all. Until this is done, AKS will get `ImagePullBackOff`.
2. **Azure OIDC federated login — no client secret stored anywhere.** An
   app registration already exists for this
   (`helmdeep-github-actions-deploy`) with a federated credential whose
   subject is `repo:huyba@6476814/helmdeep@1359057496:ref:refs/heads/main`
   (i.e. it only trusts tokens from this exact repo's `main` branch pushes).
   Note the `owner@id/repo@id` form: GitHub now presents immutable-ID
   subjects, not the classic `repo:owner/repo:...` form — a credential
   registered with the classic form fails with AADSTS700213 "No matching
   federated identity record found". If a login ever fails that way, the
   error's annotation (readable on the run's public check-run API) shows
   the exact subject GitHub presented; register that. Add
   these three as **GitHub Actions repository secrets** (Settings →
   Secrets and variables → Actions): `AZURE_CLIENT_ID`, `AZURE_TENANT_ID`,
   `AZURE_SUBSCRIPTION_ID`. None of them are secret in the sense of
   "leaking this grants access" — `AZURE_CLIENT_ID` only means anything
   paired with a token from *this exact GitHub repo's main branch*, which
   is precisely what the federated credential's `subject` pins down —
   but they're stored as Secrets anyway since Actions has nowhere else
   workflow-wide to put shared values that isn't checked into the repo.
3. **RBAC.** The app registration's service principal holds "Azure
   Kubernetes Service Cluster Admin Role" scoped to the `helmdeep-rg`
   resource group — enough to `az aks get-credentials --admin` and apply
   manifests, nothing broader. Re-run the role assignment if `helmdeep-rg`
   is ever deleted and recreated (a fresh resource group is a fresh scope).

**What the workflow does NOT do:** apply `secret.yaml`. It ships
placeholder values on purpose (see its own header) and the pipeline has no
real upstream credentials to put there. Populate real secrets once, out of
band: `kubectl create secret generic helmdeep-tool-gateway-secrets
--from-literal=<KEY>=<value>`. Every secret-backed env var in
`deployment.yaml` is `optional: true`, so a cluster with no such Secret at
all still deploys and runs — just without those upstream credentials
configured.

**Nodes and image architecture.** The workflow builds `linux/amd64` and
`linux/arm64` (`deploy/docker/Dockerfile.multiarch`, cross-compiled, no QEMU),
so the same image runs on x86 or ARM node pools. The node pool is ARM
(`Standard_B2pls_v2`, about $1/day of VM) because that is the cheapest size
this subscription's quota allows that meets AKS's 4 GB system-pool minimum.

**Verify a deployment manually** (same commands the workflow runs):

```sh
az aks get-credentials --admin --resource-group helmdeep-rg --name helmdeep-aks
kubectl get pods -l app.kubernetes.io/name=helmdeep-tool-gateway
kubectl rollout status deployment/helmdeep-tool-gateway
kubectl port-forward svc/helmdeep-tool-gateway 8443:8443
```

## Known limitations — read before using this beyond a demo

- **`replicas: 1` is load-bearing, not a default left unconsidered.**
  `internal/gateway`'s rate limiter and provenance cache are in-process
  state with no cross-pod coordination. Scaling this Deployment out today
  silently changes what "3 calls per minute" means (it becomes 3 calls per
  minute *per pod that happens to answer*) and breaks provenance
  correlation across pods. Don't raise `replicas` without addressing that
  first.
- **The audit log is on a PersistentVolumeClaim (`pvc.yaml`), but the
  default StorageClass deletes the disk with the claim.** It survives pod
  restarts and reschedules (the chain resumes from the last record — see
  `pkg/audit`), and it is a single-writer hash chain on a ReadWriteOnce
  disk, which is why the Deployment uses `strategy: Recreate` (a few
  seconds of downtime per deploy) and `replicas: 1`. But AKS's default
  class has `reclaimPolicy: Delete`: deleting the PVC or namespace deletes
  the log. Use a `Retain` class or disk snapshots before it holds evidence
  you must keep. It is also still one file on one disk, not an
  off-cluster, append-only store.
- **Health probes are real but shallow.** `/livez` only proves the process
  answers HTTP and drives the liveness probe; `/healthz` (readiness)
  checks the policy bundle is loaded and the audit log can be opened for
  append and fsynced. It does not check upstream reachability (a slow
  upstream must not pull the whole gateway out of rotation) and cannot
  detect a merely full disk — only a real write would. See
  `internal/health`.
- **`secret.yaml` ships placeholder values.** Read its own comments before
  applying it anywhere real.
- **Identity is `-dev-insecure` static tokens, not real verification.**
  `configmap.yaml` uses `identity.static_tokens`, which is unverifiable by
  design (see `docs/adr/0007-credential-broker-scope.md`) — that's why
  `deployment.yaml` passes `-dev-insecure`. Replace both with `identity.jwt`
  pointing at a real issuer once one is deployable; `internal/devissuer`
  exists as a library today but has no standalone image yet.
- **`deployment.yaml`'s own `image:` field says `:latest`**, but
  `deploy.yml` immediately overrides it with `kubectl set image` to the
  exact commit SHA that triggered the deploy — so the cluster never
  actually runs a mutable `:latest` tag once the workflow has run at least
  once. Applying `deployment.yaml` by hand (the "Try it locally" section
  above) does get you `:latest` as-is, which is fine for a throwaway kind/
  minikube cluster but not for anything you'd re-deploy repeatedly.
