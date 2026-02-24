package models

type CredentialPayload struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Provider  string `json:"provider"`
	Scope     string `json:"scope"`
	ProjectID string `json:"projectId"`
	ServiceID string `json:"serviceId"`

	AuthType   string `json:"authType"`
	Token      string `json:"token"`
	PrivateKey string `json:"privateKey"`

	RegistryUrl string `json:"registryUrl"`
	Username    string `json:"username"`
	Password    string `json:"password"`

	Notes string `json:"notes"`
}
