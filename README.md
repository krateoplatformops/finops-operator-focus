# operator-focus
This repository is part of a wider exporting architecture for the FinOps Cost and Usage Specification (FOCUS). This component is tasked with the creation of a generic exporting pipeline, according to the description given in a Custom Resource (CR). After the creation of the CR, the operator reads the FOCUS fields and creates three resources: a deployment with a generic prometheus exporter inside, a configmap containing the FOCUS report and a service that exposes the prometheus metrics. Then, it creates a new CR for another operator, which start a generic scraper that scrapes the data and uploads it to a database.

## Dependencies
To run this repository in your Kubernetes cluster, you need to have the following images in the same container registry:
 - prometheus-exporter-generic
 - prometheus-scraper-generic
 - operator-scraper

There is also the need to have an active Databricks cluster, with SQL warehouse and notebooks configured. Its login details must be placed in the database-config CR.

## Configuration
To start the exporting process, see the "config-sample.yaml" file. It includes the database-config CR.
The deployment of the operator needs a secret for the repository, called `registry-credentials` in the namespace `finops`.

The exporter container is created in the namespace of the CR. The exporter container looks for a secret in the CR namespace called `registry-credentials-default`

Detailed information on FOCUS can be found at the [official website](focus.finops.org/#specification).

## Installation
### Prerequisites
- go version v1.21.0+
- docker version 17.03+.
- kubectl version v1.11.3+.
- Access to a Kubernetes v1.30.0+ cluster.

### To Deploy on the cluster
**Build and push your image to the location specified by `IMG`:**

```sh
IMG=<some-registry>/operator-focus:tag ./scripts/docker-build-push.sh
```

**NOTE:** This image ought to be published in the personal registry you specified. 
And it is required to have access to pull the image from the working environment. 
Make sure you have the proper permission to the registry if the above commands don’t work.

**Deploy the Manager to the cluster with the image specified by `IMG`:**
**the REPO variable is mandatory. This variable points to the repository for the prometheus-exporter-generic image**

```sh
IMG=<some-registry>/operator-focus:tag REPO=<some-registry> ./scripts/deploy.sh
```

> **NOTE**: If you encounter RBAC errors, you may need to grant yourself cluster-admin 
privileges or be logged in as admin.

**Create instances of your solution**
You can apply the samples (examples) from the config/sample:

```sh
kubectl apply -k config/samples/
```

>**NOTE**: Ensure that the samples has default values to test it out.

### To Uninstall
**Delete the instances (CRs) from the cluster:**

```sh
kubectl delete -k config/samples/
```

**UnDeploy the controller from the cluster:**

```sh
./scripts/undeploy.sh
```

## License

Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
