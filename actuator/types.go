package actuator

type ServiceDescription struct {
	Name             string   `json:"name"`
	Type             string   `json:"type"`
	IsLifecycleAware bool     `json:"isLifecycleAware"`
	Dependencies     []string `json:"dependencies"`
}
