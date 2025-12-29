package env

import (
	"github.com/chronnie/governance/pkg/logger"
	"github.com/hsdfat/telco/envconfig"
)

var Env EnvConfigs

func Init() {
	err := envconfig.ReadConfigFrom("", &Env)
	if err != nil {
		logger.Log.Fatalf("Error loading env config: %v", err)
	}
}
