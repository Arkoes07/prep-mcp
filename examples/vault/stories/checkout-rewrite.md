---
type: story
title: Checkout service rewrite
ownership: full
tags: [q/hardest-project, skill/trade-off, skill/scale, domain/payments]
---
## Headline
I split a synchronous checkout into an order write plus an outbox-driven payment step, cutting p99 from 2.1 s to 340 ms.
## Beats
**Problem** Checkout called four services in sequence. **Decision** Outbox pattern. **Trade-off** Payment status became asynchronous; the UI had to poll. **Impact** p99 2.1 s to 340 ms; abandoned carts down 6%.
