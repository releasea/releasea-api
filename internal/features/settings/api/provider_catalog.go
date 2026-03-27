package settings

import (
	"net/http"

	platformmodels "releaseaapi/internal/platform/models"
	identityproviders "releaseaapi/internal/platform/providers/identity"
	notificationproviders "releaseaapi/internal/platform/providers/notifications"
	registryproviders "releaseaapi/internal/platform/providers/registry"
	scmproviders "releaseaapi/internal/platform/providers/scm"
	secretsproviders "releaseaapi/internal/platform/providers/secrets"

	"github.com/gin-gonic/gin"
)

func GetProviderCatalog(c *gin.Context) {
	c.JSON(http.StatusOK, buildProviderCatalog())
}

func buildProviderCatalog() platformmodels.ProviderCatalog {
	return platformmodels.ProviderCatalog{
		Version:       "1",
		SCM:           scmproviders.CatalogCategory(),
		Registry:      registryproviders.CatalogCategory(),
		Secrets:       secretsproviders.CatalogCategory(),
		Identity:      identityproviders.CatalogCategory(),
		Notifications: notificationproviders.CatalogCategory(),
	}
}
