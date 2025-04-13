package endpoint_test

import (
	"backendify/internal/endpoint"
	"backendify/internal/models"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestV1Response_GetCompanyResponse(t *testing.T) {
	testCases := []struct {
		name        string
		v1Response  endpoint.V1Response
		expected    models.CompanyResponse
		description string
	}{
		{
			name: "Active company without closed date",
			v1Response: endpoint.V1Response{
				Cn:        "Test Company",
				CreatedOn: "2023-01-01T00:00:00Z",
				ClosedOn:  "",
			},
			expected: models.CompanyResponse{
				Name:        "Test Company",
				Active:      true,
				ActiveUntil: "",
			},
			description: "Company with no closed date should be active",
		},
		{
			name: "Inactive company with closed date in the past",
			v1Response: endpoint.V1Response{
				Cn:        "Past Company",
				CreatedOn: "2022-01-01T00:00:00Z",
				ClosedOn:  "2022-12-31T23:59:59Z",
			},
			expected: models.CompanyResponse{
				Name:        "Past Company",
				Active:      false,
				ActiveUntil: "2022-12-31T23:59:59Z",
			},
			description: "Company with closed date in the past should be inactive",
		},
		{
			name: "Active company with closed date in the future",
			v1Response: endpoint.V1Response{
				Cn:        "Future Company",
				CreatedOn: "2023-01-01T00:00:00Z",
				ClosedOn:  time.Now().Add(time.Hour * 24 * 365).Format(time.RFC3339),
			},
			expected: models.CompanyResponse{
				Name:        "Future Company",
				Active:      true,
				ActiveUntil: time.Now().Add(time.Hour * 24 * 365).Format(time.RFC3339),
			},
			description: "Company with closed date in the future should be active",
		},
		{
			name: "Invalid closed date format",
			v1Response: endpoint.V1Response{
				Cn:        "Invalid Date Company",
				CreatedOn: "2023-01-01T00:00:00Z",
				ClosedOn:  "invalid-date",
			},
			expected: models.CompanyResponse{
				Name:        "Invalid Date Company",
				Active:      false,
				ActiveUntil: "",
			},
			description: "Company with invalid closed date format should handle error gracefully",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := tc.v1Response.GetCompanyResponse()

			// For the future date test case, we need to handle the dynamic date
			if tc.name == "Active company with closed date in the future" {
				assert.Equal(t, tc.expected.Name, result.Name)
				assert.Equal(t, tc.expected.Active, result.Active)
				// Skip exact ActiveUntil check as it's dynamically generated
			} else {
				assert.Equal(t, tc.expected, result, tc.description)
			}
		})
	}
}

func TestV2Response_GetCompanyResponse(t *testing.T) {
	testCases := []struct {
		name        string
		v2Response  endpoint.V2Response
		expected    models.CompanyResponse
		description string
	}{
		{
			name: "Active company without dissolved date",
			v2Response: endpoint.V2Response{
				CompanyName: "Test Company",
				Tin:         "123456789",
				DissolvedOn: "",
			},
			expected: models.CompanyResponse{
				Name:        "Test Company",
				Active:      true,
				ActiveUntil: "",
			},
			description: "Company with no dissolved date should be active",
		},
		{
			name: "Inactive company with dissolved date in the past",
			v2Response: endpoint.V2Response{
				CompanyName: "Past Company",
				Tin:         "987654321",
				DissolvedOn: "2022-12-31T23:59:59Z",
			},
			expected: models.CompanyResponse{
				Name:        "Past Company",
				Active:      false,
				ActiveUntil: "2022-12-31T23:59:59Z",
			},
			description: "Company with dissolved date in the past should be inactive",
		},
		{
			name: "Active company with dissolved date in the future",
			v2Response: endpoint.V2Response{
				CompanyName: "Future Company",
				Tin:         "456789123",
				DissolvedOn: time.Now().Add(time.Hour * 24 * 365).Format(time.RFC3339),
			},
			expected: models.CompanyResponse{
				Name:        "Future Company",
				Active:      true,
				ActiveUntil: time.Now().Add(time.Hour * 24 * 365).Format(time.RFC3339),
			},
			description: "Company with dissolved date in the future should be active",
		},
		{
			name: "Invalid dissolved date format",
			v2Response: endpoint.V2Response{
				CompanyName: "Invalid Date Company",
				Tin:         "135792468",
				DissolvedOn: "invalid-date",
			},
			expected: models.CompanyResponse{
				Name:        "Invalid Date Company",
				Active:      false,
				ActiveUntil: "",
			},
			description: "Company with invalid dissolved date format should handle error gracefully",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := tc.v2Response.GetCompanyResponse()

			// For the future date test case, we need to handle the dynamic date
			if tc.name == "Active company with dissolved date in the future" {
				assert.Equal(t, tc.expected.Name, result.Name)
				assert.Equal(t, tc.expected.Active, result.Active)
				// Skip exact ActiveUntil check as it's dynamically generated
			} else {
				assert.Equal(t, tc.expected, result, tc.description)
			}
		})
	}
}
