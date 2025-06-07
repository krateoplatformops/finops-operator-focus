package test

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog/log"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
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
	"github.com/krateoplatformops/provider-runtime/pkg/controller"
	"github.com/krateoplatformops/provider-runtime/pkg/logging"
	"github.com/krateoplatformops/provider-runtime/pkg/ratelimiter"

	operatorlogger "sigs.k8s.io/controller-runtime/pkg/log"

	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	operatorfocus "github.com/krateoplatformops/finops-operator-focus/internal/controller"
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
	operatorExporterControllerTag      = "0.4.1"
	exporterRegistry                   = "ghcr.io/krateoplatformops"
	exporterVersion                    = "0.4.4"

	operatorScraperControllerRegistry = "ghcr.io/krateoplatformops"
	operatorScraperControllerTag      = "0.4.0"
	scraperRegistry                   = "ghcr.io/krateoplatformops"
	scraperVersion                    = "0.4.1"
	finopsDatabaseHandlerUrl          = "http://finops-database-handler." + testNamespace + ":8088"

	cratedbHost = "cratedb." + testNamespace
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
				fmt.Sprintf("helm install finops-operator-exporter krateo/finops-operator-exporter -n %s --set controllerManager.image.repository=%s/finops-operator-exporter --set image.tag=%s --set imagePullSecrets[0].name=registry-credentials --set image.pullPolicy=Always --set env.REGISTRY=%s --set env.EXPORTER_VERSION=%s", testNamespace, operatorExporterControllerRegistry, operatorExporterControllerTag, exporterRegistry, exporterVersion),
			); p.Err() != nil {
				return ctx, fmt.Errorf("helm error while installing chart: %s %v", p.Out(), p.Err())
			}

			// install finops-operator-scraper
			if p := e2eutils.RunCommand(
				fmt.Sprintf("helm install finops-operator-scraper krateo/finops-operator-scraper -n %s --set controllerManager.image.repository=%s/finops-operator-scraper --set image.tag=%s --set imagePullSecrets[0].name=registry-credentials --set image.pullPolicy=Always --set env.REGISTRY=%s --set env.SCRAPER_VERSION=%s --set env.URL_DB_WEBSERVICE=%s", testNamespace, operatorScraperControllerRegistry, operatorScraperControllerTag, scraperRegistry, scraperVersion, finopsDatabaseHandlerUrl),
			); p.Err() != nil {
				return ctx, fmt.Errorf("helm error while installing chart: %s %v", p.Out(), p.Err())
			}

			// install cratedb-chart
			if p := e2eutils.RunCommand(
				fmt.Sprintf("helm install cratedb krateo/cratedb -n %s", testNamespace),
			); p.Err() != nil {
				return ctx, fmt.Errorf("helm error while installing chart: %s %v", p.Out(), p.Err())
			}

			// Wait for cratedb to install
			time.Sleep(30 * time.Second)

			// install finops-database-handler
			if p := e2eutils.RunCommand(
				fmt.Sprintf("helm install finops-database-handler krateo/finops-database-handler -n %s --set cratedbUserSystemName=%s, --set env.CRATE_HOST=%s", testNamespace, "cratedb-system-credentials", cratedbHost),
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

			// uninstall finops-operator-scraper
			if p := e2eutils.RunCommand(
				fmt.Sprintf("helm uninstall cratedb -n %s", testNamespace),
			); p.Err() != nil {
				return ctx, fmt.Errorf("helm error while uninstalling chart: %s %v", p.Out(), p.Err())
			}

			// uninstall finops-operator-scraper
			if p := e2eutils.RunCommand(
				fmt.Sprintf("helm uninstall finops-database-handler -n %s", testNamespace),
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
	mgrCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	controllerCreationSig := make(chan bool, 2)
	createSingle := features.New("Create single").
		WithLabel("type", "CR and resources").
		Setup(func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
			r, err := resources.New(c.Client().RESTConfig())
			if err != nil {
				t.Fatal(err)
			}

			operatorfocusapi.AddToScheme(r.GetScheme())
			r.WithNamespace(testNamespace)

			// Start the controller manager
			err = startTestManager(mgrCtx, r.GetScheme())
			if err != nil {
				t.Fatal(err)
			}

			ctx = context.WithValue(ctx, contextKey("client"), r)

			if err := wait.For(
				conditions.New(r).DeploymentAvailable("finops-operator-exporter", testNamespace),
				wait.WithTimeout(120*time.Second),
				wait.WithInterval(5*time.Second),
			); err != nil {
				log.Logger.Error().Err(err).Msg("Timed out while waiting for finops-operator-exporter deployment")
			}
			if err := wait.For(
				conditions.New(r).DeploymentAvailable("finops-operator-scraper", testNamespace),
				wait.WithTimeout(60*time.Second),
				wait.WithInterval(5*time.Second),
			); err != nil {
				log.Logger.Error().Err(err).Msg("Timed out while waiting for finops-operator-scraper deployment")
			}
			if err := wait.For(
				conditions.New(r).DeploymentAvailable("finops-database-handler", testNamespace),
				wait.WithTimeout(60*time.Second),
				wait.WithInterval(5*time.Second),
			); err != nil {
				log.Logger.Error().Err(err).Msg("Timed out while waiting for finops-operator-scraper deployment")
			}

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

			time.Sleep(5 * time.Second)
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
			case <-time.After(240 * time.Second):
				t.Fatal("Timed out wating for controller creation")
			case created := <-controllerCreationSig:
				if !created {
					t.Fatal("Operator deployment not ready")
				}
			}

			deploymentName := "all-cr-exporter"
			err := r.Get(ctx, deploymentName, testNamespace, deployment)
			if err != nil {
				deploymentName = utils.MakeGroupKeyKubeCompliant(strings.Split("finops..cratedb-config..focus_export", "..")[2]) + "-exporter"
				if len(deploymentName) > 44 {
					deploymentName = deploymentName[len(deploymentName)-44:]
				}
				err := r.Get(ctx, deploymentName+"-deployment", testNamespace, deployment)
				if err != nil {
					t.Fatal(err)
				}
			}

			ctx = context.WithValue(ctx, contextKey("deploymentName"), deploymentName)

			deployment = &appsv1.Deployment{}
			err = r.Get(ctx, deploymentName, testNamespace, deployment)
			if err != nil {
				deploymentName = "all-cr-exporter"
				if len(deploymentName) > 44 {
					deploymentName = deploymentName[len(deploymentName)-44:]
				}
				err := r.Get(ctx, deploymentName+"-deployment", testNamespace, deployment)
				if err != nil {
					t.Fatal(err)
				}
			}

			return ctx
		}).
		Assess("Value", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			deploymentName := ctx.Value(contextKey("deploymentName")).(string)

			p := e2eutils.RunCommand(fmt.Sprintf("kubectl get service -n %s %s -o 'custom-columns=ports:spec.ports[0].nodePort'", testNamespace, deploymentName+"-service"))
			if p.Err() != nil {
				t.Fatal(fmt.Errorf("error with kubectl: %s %v", p.Out(), p.Err()))
			}
			portString := new(strings.Builder)
			_, err := io.Copy(portString, p.Out())
			if err != nil {
				t.Fatal(err)
			}
			portNumber := strings.Split(portString.String(), "\n")[1]

			// open port
			go func() {
				// we ignore the error here, or you could log it
				_ = e2eutils.RunCommand(
					fmt.Sprintf("kubectl -n %s port-forward svc/%s %s:2112",
						testNamespace, deploymentName+"-service", portNumber),
				)
			}()

			time.Sleep(5 * time.Second)

			log.Debug().Msgf("curl -s %s:%s/metrics", "localhost", portNumber)

			p = e2eutils.RunCommand(fmt.Sprintf("curl -s %s:%s/metrics", "localhost", portNumber))
			if p.Err() != nil {
				t.Fatal(fmt.Errorf("error with curl: %s %v", p.Out(), p.Err()))
			}

			resultString := new(strings.Builder)
			_, err = io.Copy(resultString, p.Out())
			if err != nil {
				t.Fatal(err)
			}
			predictedOutput := `#HELPbilled_cost
#TYPEbilled_costgauge
billed_cost{AvailabilityZone="EU",BilledCost="27000",BillingAccountId="0000",BillingAccountName="testAccount",BillingCurrency="EUR",BillingPeriodEnd="2024-12-31T21:59:59Z",BillingPeriodStart="2023-12-31T22:00:00Z",CapacityReservationId="",CapacityReservationStatus="",ChargeCategory="purchase",ChargeClass="",ChargeDescription="1DellXYZ",ChargeFrequency="one-time",ChargePeriodEnd="2024-12-31T21:59:59Z",ChargePeriodStart="2023-12-31T22:00:00Z",CommitmentDiscountCategory="",CommitmentDiscountId="",CommitmentDiscountName="",CommitmentDiscountQuantity="0",CommitmentDiscountStatus="",CommitmentDiscountType="",CommitmentDiscountUnit="",ConsumedQuantity="3",ConsumedUnit="Computer",ContractedCost="27000",ContractedUnitCost="9000",EffectiveCost="30000",InvoiceIssuerName="Dell",ListCost="30000",ListUnitPrice="10000",PricingCategory="other",PricingQuantity="3",PricingUnit="machines",ProviderName="Dell",PublisherName="Dell",RegionId="",RegionName="",ResourceId="0000",ResourceName="DellHW",ResourceType="ProdCluster",ServiceCategory="Compute",ServiceName="1machinepurchase",ServiceSubcategory="test",SkuId="0000",SkuMeter="",SkuPriceDetails="",SkuPriceId="0000",SubAccountId="1234",SubAccountName="test",Tags="testkey1:testvalue;testkey2:testvalue"}27000
billed_cost{AvailabilityZone="EU",BilledCost="30000",BillingAccountId="0000",BillingAccountName="testAccount",BillingCurrency="EUR",BillingPeriodEnd="2024-12-31T21:59:59Z",BillingPeriodStart="2023-12-31T22:00:00Z",CapacityReservationId="",CapacityReservationStatus="",ChargeCategory="purchase",ChargeClass="",ChargeDescription="1DellXYZ",ChargeFrequency="one-time",ChargePeriodEnd="2024-12-31T21:59:59Z",ChargePeriodStart="2023-12-31T22:00:00Z",CommitmentDiscountCategory="",CommitmentDiscountId="",CommitmentDiscountName="",CommitmentDiscountQuantity="0",CommitmentDiscountStatus="",CommitmentDiscountType="",CommitmentDiscountUnit="",ConsumedQuantity="3",ConsumedUnit="Computer",ContractedCost="30000",ContractedUnitCost="10000",EffectiveCost="30000",InvoiceIssuerName="Dell",ListCost="30000",ListUnitPrice="10000",PricingCategory="other",PricingQuantity="3",PricingUnit="machines",ProviderName="Dell",PublisherName="Dell",RegionId="",RegionName="",ResourceId="0000",ResourceName="DellHW",ResourceType="ProdCluster",ServiceCategory="Compute",ServiceName="1machinepurchase",ServiceSubcategory="test",SkuId="0000",SkuMeter="",SkuPriceDetails="",SkuPriceId="0000",SubAccountId="1234",SubAccountName="test",Tags="testkey1:testvalue;testkey2:testvalue"}30000`
			if !strings.Contains(strings.Replace(resultString.String(), " ", "", -1), strings.Replace(predictedOutput, " ", "", -1)) {
				log.Error().Msg("unexpected exporter output")
				log.Error().Msgf("expected output: %s", predictedOutput)
				log.Error().Msgf("actual output: %s", resultString.String())
				t.Fatal(fmt.Errorf("unexpected exporter output"))
			}
			return ctx
		}).
		Assess("ValueAfterDelete", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			r := ctx.Value(contextKey("client")).(*resources.Resources)

			toDelete := &operatorfocusapi.FocusConfig{ObjectMeta: metav1.ObjectMeta{
				Name:      "focusconfig-sample2",
				Namespace: testNamespace,
			}}

			err := r.Delete(ctx, toDelete, resources.WithGracePeriod(time.Duration(1)*time.Second))
			if err != nil {
				t.Fatal(err)
			}

			time.Sleep(15 * time.Second) // Wait for the smallest polling interval period possible for the exporter

			deploymentName := ctx.Value(contextKey("deploymentName")).(string)

			p := e2eutils.RunCommand(fmt.Sprintf("kubectl get service -n %s %s -o 'custom-columns=ports:spec.ports[0].nodePort'", testNamespace, deploymentName+"-service"))
			if p.Err() != nil {
				t.Fatal(fmt.Errorf("error with kubectl: %s %v", p.Out(), p.Err()))
			}
			portString := new(strings.Builder)
			_, err = io.Copy(portString, p.Out())
			if err != nil {
				t.Fatal(err)
			}
			portNumber := strings.Split(portString.String(), "\n")[1]

			p = e2eutils.RunCommand(fmt.Sprintf("curl -s %s:%s/metrics", "localhost", portNumber))
			if p.Err() != nil {
				t.Fatal(fmt.Errorf("error with curl: %s %v", p.Out(), p.Err()))
			}

			resultString := new(strings.Builder)
			_, err = io.Copy(resultString, p.Out())
			if err != nil {
				t.Fatal(err)
			}
			predictedOutput := `#HELPbilled_cost
#TYPEbilled_costgauge
billed_cost{AvailabilityZone="EU",BilledCost="27000",BillingAccountId="0000",BillingAccountName="testAccount",BillingCurrency="EUR",BillingPeriodEnd="2024-12-31T21:59:59Z",BillingPeriodStart="2023-12-31T22:00:00Z",CapacityReservationId="",CapacityReservationStatus="",ChargeCategory="purchase",ChargeClass="",ChargeDescription="1DellXYZ",ChargeFrequency="one-time",ChargePeriodEnd="2024-12-31T21:59:59Z",ChargePeriodStart="2023-12-31T22:00:00Z",CommitmentDiscountCategory="",CommitmentDiscountId="",CommitmentDiscountName="",CommitmentDiscountQuantity="0",CommitmentDiscountStatus="",CommitmentDiscountType="",CommitmentDiscountUnit="",ConsumedQuantity="3",ConsumedUnit="Computer",ContractedCost="27000",ContractedUnitCost="9000",EffectiveCost="30000",InvoiceIssuerName="Dell",ListCost="30000",ListUnitPrice="10000",PricingCategory="other",PricingQuantity="3",PricingUnit="machines",ProviderName="Dell",PublisherName="Dell",RegionId="",RegionName="",ResourceId="0000",ResourceName="DellHW",ResourceType="ProdCluster",ServiceCategory="Compute",ServiceName="1machinepurchase",ServiceSubcategory="test",SkuId="0000",SkuMeter="",SkuPriceDetails="",SkuPriceId="0000",SubAccountId="1234",SubAccountName="test",Tags="testkey1:testvalue;testkey2:testvalue"}27000`
			if !strings.Contains(strings.Replace(resultString.String(), " ", "", -1), strings.Replace(predictedOutput, " ", "", -1)) {
				t.Fatal(fmt.Errorf("unexpected exporter output: value missing"))
			}
			deletedOutput := `billed_cost{AvailabilityZone="EU",BilledCost="30000",BillingAccountId="0000",BillingAccountName="testAccount",BillingCurrency="EUR",BillingPeriodEnd="2024-12-31T21:59:59Z",BillingPeriodStart="2023-12-31T22:00:00Z",CapacityReservationId="",CapacityReservationStatus="",ChargeCategory="purchase",ChargeClass="",ChargeDescription="1DellXYZ",ChargeFrequency="one-time",ChargePeriodEnd="2024-12-31T21:59:59Z",ChargePeriodStart="2023-12-31T22:00:00Z",CommitmentDiscountCategory="",CommitmentDiscountId="",CommitmentDiscountName="",CommitmentDiscountQuantity="0",CommitmentDiscountStatus="",CommitmentDiscountType="",CommitmentDiscountUnit="",ConsumedQuantity="3",ConsumedUnit="Computer",ContractedCost="30000",ContractedUnitCost="10000",EffectiveCost="30000",InvoiceIssuerName="Dell",ListCost="30000",ListUnitPrice="10000",PricingCategory="other",PricingQuantity="3",PricingUnit="machines",ProviderName="Dell",PublisherName="Dell",RegionId="",RegionName="",ResourceId="0000",ResourceName="DellHW",ResourceType="ProdCluster",ServiceCategory="Compute",ServiceName="1machinepurchase",ServiceSubcategory="test",SkuId="0000",SkuMeter="",SkuPriceDetails="",SkuPriceId="0000",SubAccountId="1234",SubAccountName="test",Tags="testkey1:testvalue;testkey2:testvalue"}30000`
			if strings.Contains(strings.Replace(resultString.String(), " ", "", -1), strings.Replace(deletedOutput, " ", "", -1)) {
				t.Fatal(fmt.Errorf("unexpected exporter output: deleted content still present"))
			}

			// Cleanup
			toDelete = &operatorfocusapi.FocusConfig{ObjectMeta: metav1.ObjectMeta{
				Name:      "focusconfig-sample1",
				Namespace: testNamespace,
			}}

			err = r.Delete(ctx, toDelete, resources.WithGracePeriod(time.Duration(1)*time.Second))
			if err != nil {
				t.Fatal(err)
			}
			return ctx
		}).
		Feature()

	createDual := features.New("Create dual").
		WithLabel("type", "CR and resources").
		Setup(func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
			r := ctx.Value(contextKey("client")).(*resources.Resources)

			err := decoder.DecodeEachFile(
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

			deploymentName1 := utils.MakeGroupKeyKubeCompliant(strings.Split("finops..cratedb-config..focus_export_1", "..")[2]) + "-exporter"
			deploymentName2 := utils.MakeGroupKeyKubeCompliant(strings.Split("finops..cratedb-config..focus_export_2", "..")[2]) + "-exporter"

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

	databaseCheck := features.New("Database Check").
		WithLabel("type", "Read and Upload").
		Setup(func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
			return ctx
		}).
		Assess("Upload", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			r := ctx.Value(contextKey("client")).(*resources.Resources)

			// Wait for scraper to finish
			time.Sleep(5 * time.Second)

			deployment1 := &appsv1.Deployment{}
			deploymentName1 := utils.MakeGroupKeyKubeCompliant(strings.Split("finops..cratedb-config..focus_export_1", "..")[2]) + "-exporter"
			err := r.Get(ctx, deploymentName1+"-deployment", testNamespace, deployment1)
			if err != nil {
				t.Fatal(err)
			}

			if deployment1.Status.ReadyReplicas != 1 {
				t.Fatal("Scraper replicas are not available")
			}

			deployment2 := &appsv1.Deployment{}
			deploymentName2 := utils.MakeGroupKeyKubeCompliant(strings.Split("finops..cratedb-config..focus_export_2", "..")[2]) + "-exporter"
			err = r.Get(ctx, deploymentName2+"-deployment", testNamespace, deployment2)
			if err != nil {
				t.Fatal(err)
			}

			if deployment2.Status.ReadyReplicas != 1 {
				t.Fatal("Scraper replicas are not available")
			}

			return ctx
		}).Feature()

	// test feature
	testenv.Test(t, createSingle, createDual, databaseCheck)
}

// startTestManager starts the controller manager with the given config
func startTestManager(ctx context.Context, scheme *runtime.Scheme) error {
	os.Setenv("REGISTRY", "ghcr.io/krateoplatformops")

	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string
	var secureMetrics bool
	var enableHTTP2 bool
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.BoolVar(&secureMetrics, "metrics-secure", false,
		"If set the metrics endpoint is served securely")
	flag.BoolVar(&enableHTTP2, "enable-http2", false,
		"If set, HTTP/2 will be enabled for the metrics and webhook servers")
	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	disableHTTP2 := func(c *tls.Config) {
		c.NextProtos = []string{"http/1.1"}
	}

	tlsOpts := []func(*tls.Config){}
	if !enableHTTP2 {
		tlsOpts = append(tlsOpts, disableHTTP2)
	}

	webhookServer := webhook.NewServer(webhook.Options{
		TLSOpts: tlsOpts,
	})

	namespaceCacheConfigMap := make(map[string]cache.Config)
	namespaceCacheConfigMap[testNamespace] = cache.Config{}
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress:   metricsAddr,
			SecureServing: secureMetrics,
			TLSOpts:       tlsOpts,
		},
		WebhookServer:          webhookServer,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "cb7859c8.krateo.io",
		Cache:                  cache.Options{DefaultNamespaces: namespaceCacheConfigMap},
	})
	if err != nil {
		os.Exit(1)
	}

	o := controller.Options{
		Logger:                  logging.NewLogrLogger(operatorlogger.Log.WithName("operator-focus")),
		MaxConcurrentReconciles: 1,
		PollInterval:            100,
		GlobalRateLimiter:       ratelimiter.NewGlobal(1),
	}

	if err := operatorfocus.Setup(mgr, o); err != nil {
		return err
	}

	go func() {
		if err := mgr.Start(ctx); err != nil {
			log.Error().Err(err).Msg("Failed to start manager")
		}
	}()

	return nil
}
