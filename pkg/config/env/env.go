package env

import (
	"net"

	"github.com/chronnie/governance/pkg/logger"
	"github.com/hsdfat/telco/envconfig"
)

type EnvConfigs struct {
	ConfigAddr       string `mapstructure:"CONFIG_ADDR"`
	TargetGov        string `mapstructure:"TARGET_GOV"`
	Governance       bool   `mapstructure:"GOVERNANCE"`
	ServiceIP        string `mapstructure:"SERVICE_IP"`
	ServicePort      int    `mapstructure:"SERVICE_PORT"`
	ConfigFile       string `mapstructure:"CONFIG_FILE"`
	ConfdExpose      bool   `mapstructure:"CONFD_EXPOSE"`
	ConfdServiceAddr string `mapstructure:"CONFD_SERVICE_ADDR"`
}

func (e *EnvConfigs) DefaultValues() {
	e.TargetGov = "127.0.0.1:36610"
	e.Governance = true
	e.ServiceIP = GetLocalIP()
	e.ServicePort = 36110
	e.ConfdExpose = false
	e.ConfigFile = ""
}

func (e *EnvConfigs) Print() {
	// No-op, handled elsewhere
	envconfig.Show(logger.Log.With("config", "env").(logger.Logger), e)
}

func GetLocalIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	for _, addr := range addrs {
		if ipNet, ok := addr.(*net.IPNet); ok && !ipNet.IP.IsLoopback() {
			if ipNet.IP.To4() != nil {
				return ipNet.IP.String()
			}
		}
	}
	return "0.0.0.0"
}
