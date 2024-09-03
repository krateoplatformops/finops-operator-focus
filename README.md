# FinOps Operator FOCUS
This repository is part of the wider exporting architecture for the Krateo Composable FinOps and manages custom costs in the FOCUS format.

## Summary
1. [Overview](#overview)
2. [Architecture](#architecture)
3. [Examples](#examples)
4. [Configuration](#configuration)

## Overview
This component is tasked with the creation of a generic exporting pipeline, according to the description given in a Custom Resource (CR). After the creation of the CR, the operator reads the FOCUS fields and creates a new resource for the FinOps Operator Exporter, pointing the `url` field at the Kubernetes API server and the FOCUS custom resource. This allow to create an exporter that reads directly the custom resource. The FinOps Operator Exporter then continues with the creation of the all the required resources, such as deployments, configMaps, services, and the CR for the FinOps Operator Scraper that manages scraping.

## Architecture
![Krateo Composable FinOps Operator FOCUS](resources/images/KCF-operator-focus.png)

## Examples
```yaml
apiVersion: finops.krateo.io/v1
kind: DatabaseConfig
metadata:
  name: # DatabaseConfig name
  namespace: # DatabaseConfig namespace
spec:
  host: # host name for the database
  token: # object reference to secret with key bearer-token
    name: # secret name
    namespace: # secret namespace
  clusterName: # generic compute cluster name
  notebookPath: # path to the notebook 
---
apiVersion: finops.krateo.io/v1
kind: FocusConfig
metadata:
  name: # FocusConfig name
  namespace: # FocusConfig namespace
spec:
  scraperConfig: # same fields as krateoplatformops/finops-prometheus-scraper-generic
    tableName: # tableName in the database to upload the data to
    # url: # path to the exporter, optional (if missing, its taken from the exporter)
    pollingIntervalHours: # int
    scraperDatabaseConfigRef: # See above kind DatabaseConfig
      name: # name of the databaseConfigRef CR 
      namespace: # namespace of the databaseConfigRef CR
  focusSpec: # See FOCUS for field details
    availabilityZone:
    billedCost:
    billingAccountId:
    billingAccountName:
    billingCurrency:
    billingPeriodEnd:
    billingPeriodStart:
    chargeCategory:
    chargeClass:
    chargeDescription:
    chargeFrequency:
    chargePeriodEnd:
    chargePeriodStart:
    commitmentDiscountCategory:
    commitmentDiscountName:
    commitmentDiscountStatus:
    commitmentDiscountType:
    commitmentDiscoutId:
    consumedQuantity:
    consumedUnit:
    contractedCost:
    contractedUnitCost:
    effectiveCost:
    invoiceIssuerName:
    listCost:
    listUnitPrice:
    pricingCategory:
    pricingQuantity:
    pricingUnit:
    providerName:
    publisherName:
    regionId:
    regionName:
    resourceId:
    resourceName:
    resourceType:
    serviceCategory:
    serviceName:
    skuId:
    skuPriceId:
    subAccountId:
    subAccountName:
    tags:
      - key:
        value:
```

## Configuration

### Prerequisites
- go version v1.21.0+
- docker version 17.03+.
- kubectl version v1.11.3+.
- Access to a Kubernetes v1.30.0+ cluster: Kubernetes must have the `CustomResourceFieldSelectors` feature gate enabled.

### Dependencies
To run this repository in your Kubernetes cluster, you need to have the following images in the same container registry:
 - finops-operator-exporter
 - finops-operator-scraper
 - finops-prometheus-exporter-generic
 - finops-prometheus-scraper-generic

### Configuration
There is also the need to have an active Databricks cluster, with SQL warehouse and notebooks configured. Its login details must be placed in the database-config CR.
To start the exporting process, see the "config-sample.yaml" file. It includes the database-config CR.
The deployment of the operator needs a secret for the repository, called `registry-credentials` in the namespace `finops`.

The exporter container is created in the namespace of the CR. The exporter container looks for a secret in the CR namespace called `registry-credentials-default`

Detailed information on FOCUS can be found at the [official website](https://focus.finops.org/#specification).

### Installation with HELM
```sh
$ helm repo add krateo https://charts.krateo.io
$ helm repo update krateo
$ helm install finops-operator-focus krateo/finops-operator-focus
```