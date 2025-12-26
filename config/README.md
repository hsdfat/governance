# Governance Manager Configuration

The Governance Manager uses a hierarchical configuration system with the following priority order (highest to lowest):

1. **Environment variables** (prefixed with `GOV_`)
2. **Config file specified by path** (optional)
3. **config.yaml** in standard paths (`.`, `./config`, `/etc/governance`)
4. **config.default.yaml** as fallback
5. **Hardcoded defaults**

## Configuration Files

- `config.default.yaml` - Default configuration with all available options and their default values
- `config.yaml` - Production/deployment configuration (overrides defaults)

## Configuration Sections

### Server
HTTP server configuration:
- `port`: HTTP server port (default: 8080)

### Database
PostgreSQL database configuration:
- `host`: Database host (default: localhost)
- `port`: Database port (default: 5432)
- `database`: Database name (default: governance_db)
- `username`: Database username (default: postgres)
- `password`: Database password (default: postgres)
- `sslMode`: SSL mode (disable, require, verify-ca, verify-full)
- `maxOpenConns`: Maximum open connections (default: 25)
- `maxIdleConns`: Maximum idle connections (default: 5)
- `connMaxLifetime`: Connection maximum lifetime (default: 5m)

### Manager
Governance manager behavior settings:
- `healthCheckInterval`: How often to check service health (default: 30s)
- `healthCheckTimeout`: Timeout for health check HTTP calls (default: 5s)
- `healthCheckRetry`: Number of retries before marking unhealthy (default: 3)
- `notificationInterval`: Periodic reconciliation interval (default: 60s)
- `notificationTimeout`: Timeout for notification HTTP calls (default: 5s)
- `eventQueueSize`: Event queue buffer size (default: 1000)

### Logging
Logging configuration:
- `level`: Log level (debug, info, warn, error)
- `format`: Log format (json, text)

## Environment Variables

All configuration values can be overridden using environment variables with the `GOV_` prefix.

Examples:
```bash
GOV_SERVER_PORT=8080
GOV_DATABASE_HOST=postgres.example.com
GOV_DATABASE_PORT=5432
GOV_DATABASE_DATABASE=governance
GOV_DATABASE_USERNAME=govuser
GOV_DATABASE_PASSWORD=secret
GOV_MANAGER_HEALTHCHECKINTERVAL=30s
GOV_LOGGING_LEVEL=debug
```

## Usage

The governance manager will automatically search for configuration files in the following order:
1. `config.yaml` in current directory
2. `config.yaml` in `./config` directory
3. `config.yaml` in `/etc/governance` directory
4. `config.default.yaml` as fallback

No command-line flags are needed. Simply run:
```bash
./telco_manager
```

The configuration file being used will be printed on startup.

## Example Configuration

### Development
```yaml
server:
  port: 8080

database:
  host: localhost
  port: 5432
  database: governance_dev
  username: postgres
  password: postgres
  sslMode: disable

manager:
  healthCheckInterval: 10s
  notificationInterval: 30s

logging:
  level: debug
  format: text
```

### Production
```yaml
server:
  port: 8080

database:
  host: postgres-service
  port: 5432
  database: governance_prod
  username: governance
  password: ${DB_PASSWORD}  # from environment
  sslMode: require
  maxOpenConns: 50

manager:
  healthCheckInterval: 30s
  healthCheckTimeout: 5s
  healthCheckRetry: 3
  notificationInterval: 60s
  notificationTimeout: 5s

logging:
  level: info
  format: json
```
