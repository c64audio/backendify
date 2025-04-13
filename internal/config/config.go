package config

type Config struct {
	ServerConfig         ServerConfiguration `json:"server"`
	RetryDelays          []int               `json:"retry_delays"`
	EndpointPathTemplate string              `json:"endpoint_path"` // assumed the same for both endpoints
	DefaultCacheSize     int                 `json:"default_cache_size"`
	EndpointTimeout      float64             `json:"endpoint_timeout"` // endpoints are given a more luxurious timeout
	SpawnLocalhostMocks  bool                `json:"spawn_localhost_mocks"`
}

type ServerConfiguration struct {
	Port         int     `json:"port"`
	MaxWorkers   int     `json:"max_workers"`
	SLA          float64 `json:"sla"`
	QueueTimeout int     `json:"queue_timeout"`
}
