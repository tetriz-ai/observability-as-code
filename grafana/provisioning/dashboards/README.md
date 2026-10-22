# Grafana Dashboard Provisioning

This is the configuration for Grafana's file-based dashboard provider. On startup it points
Grafana at the generated JSON, which `docker-compose.yml` mounts at `GF_DASHBOARDS_PATH`.
