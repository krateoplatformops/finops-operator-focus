/*
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
*/
// +kubebuilder:object:generate=true
package v1

import (
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	operatorPackage "github.com/krateoplatformops/finops-operator-exporter/api/v1"
)

// FocusConfigSpec defines the desired state of FocusConfig
type FocusConfigSpec struct {
	FocusSpec FocusSpec `yaml:"focusSpec" json:"focusSpec"`
	// +optional
	ScraperConfig operatorPackage.ScraperConfig `yaml:"scraperConfig" json:"scraperConfig"`
}

// FocusConfigStatus defines the observed state of FocusConfig
type FocusConfigStatus struct {
	GroupKey string `json:"groupKey,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// FocusConfig is the Schema for the focusconfigs API
type FocusConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   FocusConfigSpec   `json:"spec,omitempty"`
	Status FocusConfigStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// FocusConfigList contains a list of FocusConfig
type FocusConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []FocusConfig `json:"items"`
}

func init() {
	SchemeBuilder.Register(&FocusConfig{}, &FocusConfigList{})
}

type TagsType struct {
	Key   string `yaml:"key" json:"key"`
	Value string `yaml:"value" json:"value"`
}

type FocusSpec struct {
	// +optional
	AvailabilityZone string            `yaml:"availabilityZone" json:"availabilityZone"`
	BilledCost       resource.Quantity `yaml:"billedCost" json:"billedCost"`
	// +optional
	BillingAccountId string `yaml:"billingAccountId" json:"billingAccountId"`
	// +optional
	BillingAccountName string `yaml:"billingAccountName" json:"billingAccountName"`
	// +kubebuilder:validation:Pattern=`^[A-Z]{3}$`
	BillingCurrency    string      `yaml:"billingCurrency" json:"billingCurrency"`
	BillingPeriodEnd   metav1.Time `yaml:"billingPeriodEnd" json:"billingPeriodEnd"`
	BillingPeriodStart metav1.Time `yaml:"billingPeriodStart" json:"billingPeriodStart"`
	// +kubebuilder:validation:Pattern=`(\b[Aa]djustment\b)|(\b[Pp]urchase\b)|(\b[Tt]ax\b)|(\b[Uu]sage\b)`
	ChargeCategory    string `yaml:"chargeCategory" json:"chargeCategory"`
	ChargeDescription string `yaml:"chargeDescription" json:"chargeDescription"`
	// +kubebuilder:validation:Pattern=`(\b[Oo]ne-{0,1}[Tt]ime\b)|(\b[Rr]ecurring\b)|(\b[Uu]sage-{0,1}[Bb]ased\b)`
	// +optional
	ChargeFrequency   string      `yaml:"chargeFrequency" json:"chargeFrequency"`
	ChargePeriodEnd   metav1.Time `yaml:"chargePeriodEnd" json:"chargePeriodEnd"`
	ChargePeriodStart metav1.Time `yaml:"chargePeriodStart" json:"chargePeriodStart"`
	// +kubebuilder:validation:Pattern=`(\b[Oo]n-{0,1}[Dd]emand\b)|(\b[Uu]sed {0,1}[Cc]ommitment\b)|(\b[Uu]nsed {0,1}[Cc]ommitment\b)|(\b[Rr]efund\b)|(\b[Cc]redit\b)|(\b[Rr]ounding {0,1}[Ee]rror\b)|(\b[Gg]eneral {0,1}[Aa]djustment\b)`
	// +optional
	ChargeSubCategory string `yaml:"chargeSubCategory" json:"chargeSubCategory"`
	// +kubebuilder:validation:Pattern=`(\b[Ss]spend\b)|(\b[Uu]sage\b)`
	// +optional
	CommitmentDiscountCategory string `yaml:"commitmentDiscountCategory" json:"commitmentDiscountCategory"`
	// +optional
	CommitmentDiscountId string `yaml:"commitmentDiscoutId" json:"commitmentDiscoutId"`
	// +optional
	CommitmentDiscountType string `yaml:"commitmentDiscountType" json:"commitmentDiscountType"`
	// +optional
	CommitmentDiscountName string `yaml:"commitmentDiscountName" json:"commitmentDiscountName"`
	// +optional
	EffectiveCost resource.Quantity `yaml:"effectiveCost" json:"effectiveCost"`
	InvoiceIssuer string            `yaml:"invoiceIssuer" json:"invoiceIssuer"`
	// +optional
	ListCost resource.Quantity `yaml:"listCost" json:"listCost"`
	// +optional
	ListUnitPrice resource.Quantity `yaml:"listUnitPrice" json:"listUnitPrice"`
	// +kubebuilder:validation:Pattern=`(\b[Oo]n-{0,1}[Dd]emand\b)|(\b[Dd]ynamic\b)|(\b[Cc]ommitment-{0,1}[Bb]ased\b)|(\b[Oo]ther\b)`
	// +optional
	PricingCategory string `yaml:"pricingCategory" json:"pricingCategory"`
	// +optional
	PricingQuantity resource.Quantity `yaml:"pricingQuantity" json:"pricingQuantity"`
	// +optional
	PricingUnit string `yaml:"pricingUnit" json:"pricingUnit"`
	// +optional
	Provider string `yaml:"provider" json:"provider"`
	// +optional
	Publisher string `yaml:"publisher" json:"publisher"`
	// +optional
	Region string `yaml:"region" json:"region"`
	// +optional
	ResourceId string `yaml:"resourceId" json:"resourceId"`
	// +optional
	ResourceName string `yaml:"resourceName" json:"resourceName"`
	// +optional
	ResourceType string `yaml:"resourceType" json:"resourceType"`
	// +kubebuilder:validation:Pattern=`(\bAI and Machine Learning\b)|(\bAnalytics\b)|(\bBusiness\b)|(\bCompute\b)|(\bDatabases\b)|(\bDeveloper Tools\b)|(\bMulticloud\b)|(\bIdentity\b)|(\bIntegration\b)|(\bInternet of Things\b)|(\bManagement and Governance\b)|(\bMedia\b)|(\bMigration\b)|(\bMobile\b)|(\bNetworking\b)|(\bSecurity\b)|(\bStorage\b)|(\bWeb\b)|(\bOther\b)`
	// +optional
	ServiceCategory string `yaml:"serviceCategory" json:"serviceCategory"`
	// +optional
	ServiceName string `yaml:"serviceName" json:"serviceName"`
	// +optional
	SkuId string `yaml:"skuId" json:"skuId"`
	// +optional
	SkuPriceId string `yaml:"skuPriceId" json:"skuPriceId"`
	// +optional
	SubAccountId string `yaml:"subAccountId" json:"subAccountId"`
	// +optional

	SubAccountName string `yaml:"subAccountName" json:"subAccountName"`
	// +optional
	Tags          []TagsType        `yaml:"tags" json:"tags"`
	UsageQuantity resource.Quantity `yaml:"usageQuantity" json:"usageQuantity"`
	UsageUnit     string            `yaml:"usageUnit" json:"usageUnit"`
}
