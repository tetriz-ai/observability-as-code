#!/usr/bin/env python3
# Copyright 2026 Workloom Solutions Private Limited
# SPDX-License-Identifier: MIT
"""Synthetic asynq metric series for the local README preview, not production data."""
from http.server import BaseHTTPRequestHandler, HTTPServer
import math
import time

START = time.monotonic()

class Metrics(BaseHTTPRequestHandler):
    def do_GET(self):
        elapsed = time.monotonic() - START
        pending = int(40 + 10 * math.sin(elapsed / 15))
        values = {
            'asynq_queue_size{queue="exports"}': pending + 6,
            'asynq_queue_latency_seconds{queue="exports"}': 12,
            'asynq_queue_memory_usage_approx_bytes{queue="exports"}': 2097152,
            'asynq_queue_paused_total{queue="exports"}': 0,
            'asynq_tasks_processed_total{queue="exports"}': int(elapsed * 12),
            'asynq_tasks_failed_total{queue="exports"}': int(elapsed * 0.06),
        }
        for state, count in dict(pending=pending, active=4, scheduled=2, retry=0, archived=0, completed=0).items():
            values[f'asynq_tasks_enqueued_total{{queue="exports",state="{state}"}}'] = count
        body = ('\n'.join(f'{key} {value}' for key, value in values.items()) + '\n').encode()
        self.send_response(200)
        self.send_header('Content-Type', 'text/plain; version=0.0.4')
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, *_args):
        pass

HTTPServer(('0.0.0.0', 8080), Metrics).serve_forever()
