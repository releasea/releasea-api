package secretsproviders

import platformmodels "releaseaapi/internal/platform/models"

func CatalogCategory() platformmodels.ProviderCategory {
	return platformmodels.ProviderCategory{
		Kind:            "secrets",
		Label:           "Secrets",
		Description:     "Secrets managers available for resolving secret references during deploys.",
		DefaultProvider: "vault",
		Providers: []platformmodels.ProviderDefinition{
			{
				ID:           "vault",
				Label:        "Vault",
				Description:  "HashiCorp Vault backed secret resolution.",
				Capabilities: []string{"secret-read", "secret-reference"},
				ConfigFields: []string{"address", "token"},
			},
			{
				ID:           "aws",
				Label:        "AWS Secrets Manager",
				Description:  "AWS-hosted secret resolution using access key credentials.",
				Capabilities: []string{"secret-read", "secret-reference"},
				ConfigFields: []string{"accessKeyId", "secretAccessKey", "region"},
			},
			{
				ID:           "gcp",
				Label:        "GCP Secret Manager",
				Description:  "Google Cloud secret resolution using service account credentials.",
				Capabilities: []string{"secret-read", "secret-reference"},
				ConfigFields: []string{"projectId", "serviceAccountJson"},
			},
		},
	}
}
