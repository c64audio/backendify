// Package endpoint Create an IEndpoint to allow for mocking
package endpoint

import (
	"backendify/internal/models"
	"backendify/utils"
)

type IEndpoint interface {
	FetchCompany(client utils.HTTPClient, id string) (models.CompanyResponse, int, error)
	Close()
	// Add any other methods that the Endpoint should provide
}

// Ensure Endpoint implements IEndpoint
var _ IEndpoint = (*Endpoint)(nil)
