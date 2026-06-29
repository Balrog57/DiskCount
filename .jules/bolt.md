## 2024-06-29 - [Avoid sequential strings.ReplaceAll]
**Learning:** Sequential calls to `strings.ReplaceAll` cause unnecessary string allocations which slow down text parsing.
**Action:** When a function removes or changes specific characters sequentially, use `strings.ContainsAny` for a fast-path skip, and a single-pass `strings.Builder` byte iteration loop to minimize GC overhead and improve execution speed.
