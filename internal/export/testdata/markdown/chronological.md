# Checkout latency spike (checkout-spike)

- **Refs:** INC-4821, https://status.example.com/incidents/4821
- **Window:** 2026-09-10 23:58:12 → 2026-09-11 00:08 UTC
- **Events:** 4 · **Sources:** `mongod`, `slack` · **Tags:** `#db` `#election`
- **Exported:** 2026-09-12 21:40 UTC by coroner 0.1.0

Annotations on the whole timeline:

- _comment (sam):_ Started from the checkout alert.
- evidence (julian): `PagerDuty incident 4821 opened` ![incident.png](attachments/d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4.png)

## 2026-09-10 (UTC)

- **23:58:12** · `slack` · maria: checkout p99 is 4s
  - _comment (sam):_ Lines up with the deploy. [deploys.csv](attachments/e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5.csv)
- **~23:59** · `mongod` · Slow query on checkout.orders: 2150 ms `#db`
  - evidence (julian): `{"t":{"$date":"2026-09-10T23:59:41.412+00:00"},"msg":"Slow query"}`

## 2026-09-11 (UTC)

- **00:02:30–00:05:40** · `mongod` · Replica set has no primary `#db` `#election`
  - ![p99 latency.png](attachments/a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1.png)

  - [trace.pdf](attachments/b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2.pdf)

  - evidence (julian): ![rs-status.png](attachments/c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3.png)

    ```text
    rs.status()
    {
      "ok": 0,
      "errmsg": "no primary"
    }
    ```

  - _comment:_ Election took three minutes.

- **00:07** · Rolled back deploy 812
