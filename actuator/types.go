package actuator

type ServiceDescription struct {
	Name         string   `json:"name"`
	Type         string   `json:"type"`
	IsStartAware bool     `json:"isStartAware"`
	IsStopAware  bool     `json:"isStopAware"`
	Dependencies []string `json:"dependencies"`
}
