# README visual

Rebuild from the repository root with Python 3.10+, Pillow 11+ and Arial/Menlo or DejaVu fonts:

```sh
python3 .github/assets/demo/render.py
```

`scenes.json` is the editable storyboard. This is an illustrated walkthrough, not a recording of a hosted product. Local example outputs and reproduction commands are in `evidence.txt`. No production credentials or customer data are used.

To reproduce the dashboard screenshot with synthetic metrics:

```sh
make dashboards
GRAFANA_PORT=13300 PROMETHEUS_PORT=19091 docker compose -p readme-observability -f docker-compose.yml -f .github/assets/demo/compose.yaml up -d --wait
```

Open `http://127.0.0.1:13300/d/asynq-queue-health?from=now-5m&to=now`, close the navigation menu and expand Throughput. Allow enough scrapes for the rate panels, then save a screenshot of Overview and the first Throughput row as `grafana.png` here. These metrics are synthetic fixtures, not measured production performance. Stop only this stack with the same compose arguments followed by `down -v`.
