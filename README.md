# finops-operator-focus

A Kubernetes operator that manages custom costs in the FOCUS format, creating exporting pipelines by bridging FOCUS Custom Resources with the FinOps Operator Exporter and Scraper components.

📖 **Full documentation**: [docs.krateo.io — finops-operator-focus](https://docs.krateo.io/key-concepts/kcf/finops-components/finops-operator-focus)

---

## Key features

- Translates `FocusConfig` Custom Resources into full exporting pipelines, including deployments, configmaps, services, and scraper CRs
- Reads FOCUS-compliant cost data directly from the Kubernetes API server via the FinOps Operator Exporter
- Supports all standard FOCUS fields including billing, charge, commitment discount, pricing, and resource metadata

## Requirements

| Dependency | Minimum version |
|------------|----------------|
| Kubernetes | v1.31.0 (`CustomResourceFieldSelectors` feature gate required) |
| Krateo | v3.0.0 |
| finops-operator-exporter | v0.5.1 |
| finops-operator-scraper | v0.5.0 |

## Install

```bash
helm repo add krateo https://charts.krateo.io
helm repo update
helm install finops-operator-focus krateo/finops-operator-focus --namespace krateo-system --create-namespace
```

> For advanced installation options, custom values, and upgrade instructions, see the [installation guide](https://docs.krateo.io/key-concepts/kcf/finops-components/finops-operator-focus).

## Environment variables

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| — | — | — | No environment variables are documented for this component |