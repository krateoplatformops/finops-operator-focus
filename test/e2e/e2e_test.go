package test

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	"sigs.k8s.io/e2e-framework/klient/decoder"
	"sigs.k8s.io/e2e-framework/klient/k8s/resources"
	"sigs.k8s.io/e2e-framework/klient/wait"
	"sigs.k8s.io/e2e-framework/klient/wait/conditions"
	"sigs.k8s.io/e2e-framework/pkg/env"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/envfuncs"
	"sigs.k8s.io/e2e-framework/pkg/features"
	"sigs.k8s.io/e2e-framework/support/kind"
	e2eutils "sigs.k8s.io/e2e-framework/support/utils"

	operatorfocusapi "github.com/krateoplatformops/finops-operator-focus/api/v1"
	"github.com/krateoplatformops/finops-operator-focus/internal/utils"
)

type contextKey string

var (
	testenv env.Environment
)

const (
	testNamespace   = "finops-test" // If you changed this test environment, you need to change the RoleBinding in the "deploymentsPath" folder
	crdsPath        = "../../config/crd/bases"
	deploymentsPath = "./manifests/deployments"
	toTest          = "./manifests/to_test/"

	testName = "focusconfig-sample"

	operatorExporterControllerRegistry = "ghcr.io/krateoplatformops"
	operatorExporterControllerTag      = "0.3.2"
	exporterRegistry                   = "ghcr.io/krateoplatformops"

	operatorScraperControllerRegistry = "ghcr.io/krateoplatformops"
	operatorScraperControllerTag      = "0.3.1"
	scraperRegistry                   = "ghcr.io/krateoplatformops"
)

func TestMain(m *testing.M) {
	testenv = env.New()
	kindClusterName := "krateo-test"

	// Use pre-defined environment funcs to create a kind cluster prior to test run
	testenv.Setup(
		envfuncs.CreateCluster(kind.NewProvider(), kindClusterName),
		envfuncs.CreateNamespace(testNamespace),
		envfuncs.SetupCRDs(crdsPath, "*"),
		func(ctx context.Context, cfg *envconf.Config) (context.Context, error) {
			// setup Krateo's helm
			if p := e2eutils.RunCommand("helm repo add krateo https://charts.krateo.io"); p.Err() != nil {
				return ctx, fmt.Errorf("helm error while adding repository: %s %v", p.Out(), p.Err())
			}

			if p := e2eutils.RunCommand("helm repo update krateo"); p.Err() != nil {
				return ctx, fmt.Errorf("helm error while updating helm: %s %v", p.Out(), p.Err())
			}

			// install finops-operator-exporter
			if p := e2eutils.RunCommand(
				fmt.Sprintf("helm install finops-operator-exporter krateo/finops-operator-exporter -n %s --set controllerManager.image.repository=%s/finops-operator-exporter --set image.tag=%s --set imagePullSecrets[0].name=registry-credentials --set image.pullPolicy=Always --set env.REGISTRY=%s", testNamespace, operatorExporterControllerRegistry, operatorExporterControllerTag, exporterRegistry),
			); p.Err() != nil {
				return ctx, fmt.Errorf("helm error while installing chart: %s %v", p.Out(), p.Err())
			}

			// install finops-operator-scraper
			if p := e2eutils.RunCommand(
				fmt.Sprintf("helm install finops-operator-scraper krateo/finops-operator-scraper -n %s --set controllerManager.image.repository=%s/finops-operator-scraper --set image.tag=%s --set imagePullSecrets[0].name=registry-credentials --set image.pullPolicy=Always --set env.REGISTRY=%s", testNamespace, operatorScraperControllerRegistry, operatorScraperControllerTag, scraperRegistry),
			); p.Err() != nil {
				return ctx, fmt.Errorf("helm error while installing chart: %s %v", p.Out(), p.Err())
			}
			return ctx, nil
		},
	)

	// Use pre-defined environment funcs to teardown kind cluster after tests
	testenv.Finish(
		envfuncs.DeleteNamespace(testNamespace),
		envfuncs.TeardownCRDs(crdsPath, "*"),
		func(ctx context.Context, cfg *envconf.Config) (context.Context, error) {
			// uninstall finops-operator-exporter
			if p := e2eutils.RunCommand(
				fmt.Sprintf("helm uninstall finops-operator-exporter -n %s", testNamespace),
			); p.Err() != nil {
				return ctx, fmt.Errorf("helm error while uninstalling chart: %s %v", p.Out(), p.Err())
			}

			// uninstall finops-operator-scraper
			if p := e2eutils.RunCommand(
				fmt.Sprintf("helm uninstall finops-operator-scraper -n %s", testNamespace),
			); p.Err() != nil {
				return ctx, fmt.Errorf("helm error while uninstalling chart: %s %v", p.Out(), p.Err())
			}
			return ctx, nil
		},
		envfuncs.DestroyCluster(kindClusterName),
	)

	// launch package tests
	os.Exit(testenv.Run(m))
}

func TestFOCUS(t *testing.T) {
	controllerCreationSig := make(chan bool, 2)
	createSingle := features.New("Create single").
		WithLabel("type", "CR and resources").
		Setup(func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
			r, err := resources.New(c.Client().RESTConfig())
			if err != nil {
				t.Fail()
			}
			operatorfocusapi.AddToScheme(r.GetScheme())
			r.WithNamespace(testNamespace)

			ctx = context.WithValue(ctx, contextKey("client"), r)

			err = decoder.DecodeEachFile(
				ctx, os.DirFS(deploymentsPath), "*",
				decoder.CreateHandler(r),
				decoder.MutateNamespace(testNamespace),
			)
			if err != nil {
				t.Fatalf("Failed due to error: %s", err)
			}

			err = decoder.DecodeEachFile(
				ctx, os.DirFS(toTest), "*1.yaml",
				decoder.CreateHandler(r),
				decoder.MutateNamespace(testNamespace),
			)
			if err != nil {
				t.Fatalf("Failed due to error: %s", err)
			}

			if err := wait.For(
				conditions.New(r).DeploymentAvailable("finops-operator-exporter-controller-manager", testNamespace),
				wait.WithTimeout(120*time.Second),
				wait.WithInterval(5*time.Second),
			); err != nil {
				log.Printf("Timed out while waiting for finops-operator-exporter deployment: %s", err)
			}
			if err := wait.For(
				conditions.New(r).DeploymentAvailable("finops-operator-scraper-controller-manager", testNamespace),
				wait.WithTimeout(60*time.Second),
				wait.WithInterval(5*time.Second),
			); err != nil {
				log.Printf("Timed out while waiting for finops-operator-scraper deployment: %s", err)
			}
			if err := wait.For(
				conditions.New(r).DeploymentAvailable("finops-operator-focus-controller-manager", testNamespace),
				wait.WithTimeout(30*time.Second),
				wait.WithInterval(5*time.Second),
			); err != nil {
				log.Printf("Timed out while waiting for finops-operator-focus deployment: %s", err)
			}
			controllerCreationSig <- true
			return ctx
		}).
		Assess("CR", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			r := ctx.Value(contextKey("client")).(*resources.Resources)

			crGet := &operatorfocusapi.FocusConfig{}
			err := r.Get(ctx, testName+"1", testNamespace, crGet)
			if err != nil {
				t.Fatal(err)
			}

			err = r.Get(ctx, testName+"2", testNamespace, crGet)
			if err != nil {
				t.Fatal(err)
			}
			return ctx
		}).
		Assess("Resources", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			r := ctx.Value(contextKey("client")).(*resources.Resources)

			deployment := &appsv1.Deployment{}

			select {
			case <-time.After(180 * time.Second):
				t.Fatal("Timed out wating for controller creation")
			case created := <-controllerCreationSig:
				if !created {
					t.Fatal("Operator deployment not ready")
				}
			}

			deploymentName := "all-cr-exporter"
			err := r.Get(ctx, deploymentName, testNamespace, deployment)
			if err != nil {
				deploymentName = utils.MakeGroupKeyKubeCompliant(strings.Split("finops>cratedb-config>focus_export", ">")[2]) + "-exporter"
				if len(deploymentName) > 44 {
					deploymentName = deploymentName[len(deploymentName)-44:]
				}
				err := r.Get(ctx, deploymentName+"-deployment", testNamespace, deployment)
				if err != nil {
					t.Fatal(err)
				}
			}

			ctx = context.WithValue(ctx, contextKey("deploymentName"), deploymentName)

			return ctx
		}).
		Assess("Value", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			deploymentName := ctx.Value(contextKey("deploymentName")).(string)

			p := e2eutils.RunCommand(fmt.Sprintf("kubectl get service -n %s %s -o custom-columns=ports:spec.ports[0].nodePort", testNamespace, deploymentName+"-service"))
			if p.Err() != nil {
				t.Fatal(fmt.Errorf("error with kubectl: %s %v", p.Out(), p.Err()))
			}
			portString := new(strings.Builder)
			_, err := io.Copy(portString, p.Out())
			if err != nil {
				t.Fatal(err)
			}
			portNumber := strings.Split(portString.String(), "\n")[1]

			p = e2eutils.RunCommand("kubectl get nodes -o custom-columns=status:status.addresses[0].address")
			if p.Err() != nil {
				t.Fatal(fmt.Errorf("error with kubectl: %s %v", p.Out(), p.Err()))
			}
			addressString := new(strings.Builder)
			_, err = io.Copy(addressString, p.Out())
			if err != nil {
				t.Fatal(err)
			}
			address := strings.Split(addressString.String(), "\n")[1]

			p = e2eutils.RunCommand(fmt.Sprintf("curl -s %s:%s/metrics", address, portNumber))
			if p.Err() != nil {
				t.Fatal(fmt.Errorf("error with curl: %s %v", p.Out(), p.Err()))
			}

			resultString := new(strings.Builder)
			_, err = io.Copy(resultString, p.Out())
			if err != nil {
				t.Fatal(err)
			}
			predictedOutput := `#HELPbilled_cost__1
#TYPEbilled_cost__1gauge
billed_cost__1{AvailabilityZone="EU",BilledCost="27000",BillingAccountId="0000",BillingAccountName="testAccount",BillingCurrency="EUR",BillingPeriodEnd="2024-12-31T21:59:59Z",BillingPeriodStart="2023-12-31T22:00:00Z",ChargeCategory="purchase",ChargeClass="",ChargeDescription="1DellXYZ",ChargeFrequency="one-time",ChargePeriodEnd="2024-12-31T21:59:59Z",ChargePeriodStart="2023-12-31T22:00:00Z",CommitmentDiscountCategory="",CommitmentDiscountId="",CommitmentDiscountName="",CommitmentDiscountStatus="",CommitmentDiscountType="",ConsumedQuantity="3",ConsumedUnit="Computer",ContractedCost="27000",ContractedUnitCost="9000",EffectiveCost="30000",InvoiceIssuerName="Dell",ListCost="30000",ListUnitPrice="10000",PricingCategory="other",PricingQuantity="3",PricingUnit="machines",ProviderName="Dell",PublisherName="Dell",RegionId="",RegionName="",ResourceId="0000",ResourceName="DellHW",ResourceType="ProdCluster",ServiceCategory="Compute",ServiceName="1machinepurchase",SkuId="0000",SkuPriceId="0000",SubAccountId="1234",SubAccountName="test",Tags="testkey1:testvalue;testkey2:testvalue"}27000
#HELPbilled_cost__2
#TYPEbilled_cost__2gauge
billed_cost__2{AvailabilityZone="EU",BilledCost="30000",BillingAccountId="0000",BillingAccountName="testAccount",BillingCurrency="EUR",BillingPeriodEnd="2024-12-31T21:59:59Z",BillingPeriodStart="2023-12-31T22:00:00Z",ChargeCategory="purchase",ChargeClass="",ChargeDescription="1DellXYZ",ChargeFrequency="one-time",ChargePeriodEnd="2024-12-31T21:59:59Z",ChargePeriodStart="2023-12-31T22:00:00Z",CommitmentDiscountCategory="",CommitmentDiscountId="",CommitmentDiscountName="",CommitmentDiscountStatus="",CommitmentDiscountType="",ConsumedQuantity="3",ConsumedUnit="Computer",ContractedCost="30000",ContractedUnitCost="10000",EffectiveCost="30000",InvoiceIssuerName="Dell",ListCost="30000",ListUnitPrice="10000",PricingCategory="other",PricingQuantity="3",PricingUnit="machines",ProviderName="Dell",PublisherName="Dell",RegionId="",RegionName="",ResourceId="0000",ResourceName="DellHW",ResourceType="ProdCluster",ServiceCategory="Compute",ServiceName="1machinepurchase",SkuId="0000",SkuPriceId="0000",SubAccountId="1234",SubAccountName="test",Tags="testkey1:testvalue;testkey2:testvalue"}30000`
			if !strings.Contains(strings.Replace(resultString.String(), " ", "", -1), strings.Replace(predictedOutput, " ", "", -1)) {
				t.Fatal(fmt.Errorf("unexpected exporter output"))
			}
			return ctx
		}).Feature()

	createDual := features.New("Create dual").
		WithLabel("type", "CR and resources").
		Setup(func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
			r, err := resources.New(c.Client().RESTConfig())
			if err != nil {
				t.Fail()
			}
			operatorfocusapi.AddToScheme(r.GetScheme())
			r.WithNamespace(testNamespace)

			ctx = context.WithValue(ctx, contextKey("client"), r)

			err = decoder.DecodeEachFile(
				ctx, os.DirFS(toTest), "*2.yaml",
				decoder.CreateHandler(r),
				decoder.MutateNamespace(testNamespace),
			)
			if err != nil {
				t.Fatalf("Failed due to error: %s", err)
			}

			return ctx
		}).
		Assess("CR", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			r := ctx.Value(contextKey("client")).(*resources.Resources)

			crGet := &operatorfocusapi.FocusConfig{}
			err := r.Get(ctx, testName+"3", testNamespace, crGet)
			if err != nil {
				t.Fatal(err)
			}

			err = r.Get(ctx, testName+"4", testNamespace, crGet)
			if err != nil {
				t.Fatal(err)
			}
			return ctx
		}).
		Assess("Resources", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			r := ctx.Value(contextKey("client")).(*resources.Resources)

			// Give time to the exporter/scraper to start
			time.Sleep(5 * time.Second)

			deployment1 := &appsv1.Deployment{}
			deployment2 := &appsv1.Deployment{}

			deploymentName1 := utils.MakeGroupKeyKubeCompliant(strings.Split("finops>cratedb-config>focus_export_1", ">")[2]) + "-exporter"
			deploymentName2 := utils.MakeGroupKeyKubeCompliant(strings.Split("finops>cratedb-config>focus_export_2", ">")[2]) + "-exporter"

			if len(deploymentName1) > 44 {
				deploymentName1 = deploymentName1[len(deploymentName1)-44:]
			}

			if len(deploymentName2) > 44 {
				deploymentName2 = deploymentName2[len(deploymentName2)-44:]
			}

			err := r.Get(ctx, deploymentName1+"-deployment", testNamespace, deployment1)
			if err != nil {
				t.Fatal(err)
			}

			err = r.Get(ctx, deploymentName2+"-deployment", testNamespace, deployment2)
			if err != nil {
				t.Fatal(err)
			}

			return ctx
		}).Feature()

	// test feature
	testenv.Test(t, createSingle, createDual)
}
