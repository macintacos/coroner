# mongo-failover + checkout-latency

- **Timelines:**
  - `mongo-failover` · MongoDB primary failover · **Refs:** INC-4820
  - `checkout-latency` · Checkout latency · **Refs:** INC-4821, https://status.example.com/incidents/4821
- **Window:** 2026-09-10 14:01:30 → 14:08 UTC
- **Events:** 6 · **Sources:** `k8s:events`, `mongod`, `slack` · **Tags:** `#db` `#deploy`
- **Exported:** 2026-09-12 21:40 UTC by coroner 0.1.0

Annotations on a whole timeline:

- `mongo-failover` · _comment (sam):_ Started from the replica set alert.
- `checkout-latency` · evidence (julian): `PagerDuty incident 4821 opened`

## 2026-09-10 (UTC)

- **14:01:30** · `checkout-latency` · `slack` · maria: checkout p99 is 4s
- **14:02:03** · `mongo-failover` · `mongod` · Primary stepped down `#db`
  - evidence: `replSetStepDown in the mongod log`
- **14:02:40** · `mongo-failover` · `k8s:events` · db-2 pod restarted
- **14:02:40** · `checkout-latency` · `k8s:events` · checkout-api scaled to 12 pods
- **14:03:00–14:05:10** · `mongo-failover` · Replica set has no primary
- **14:07** · `checkout-latency` · Rolled back deploy 812 `#deploy`
