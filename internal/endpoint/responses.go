package endpoint

import (
	"backendify/internal/models"
	"time"
)

type V1Response struct {
	Cn        string `json:"cn"`
	CreatedOn string `json:"created_on"`
	ClosedOn  string `json:"closed_on"`
}

type V2Response struct {
	CompanyName string `json:"company_name"`
	Tin         string `json:"tin"`
	DissolvedOn string `json:"dissolved_on,omitempty"`
}

func (v1 *V1Response) GetCompanyResponse() models.CompanyResponse {

	result := models.CompanyResponse{
		Name: v1.Cn,
	}

	if v1.ClosedOn != "" {
		t, err := time.Parse(time.RFC3339, v1.ClosedOn)
		if err == nil {
			result.Active = time.Now().Before(t)
			result.ActiveUntil = v1.ClosedOn
		}
	} else {
		result.Active = true
	}
	return result
}

func (v2 *V2Response) GetCompanyResponse() models.CompanyResponse {
	result := models.CompanyResponse{
		Name: v2.CompanyName,
	}
	if v2.DissolvedOn != "" {
		t, err := time.Parse(time.RFC3339, v2.DissolvedOn)
		if err == nil {
			result.Active = time.Now().Before(t)
			result.ActiveUntil = v2.DissolvedOn
		}
	} else {
		result.Active = true
	}
	return result
}
