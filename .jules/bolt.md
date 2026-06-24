## 2026-06-07 - Avoid Sequential strings.ReplaceAll in Hot Paths
**Learning:** In Go, executing multiple `strings.ReplaceAll` calls sequentially on the same string allocates a new string object and iterates over the underlying bytes on each call, leading to measurable GC pressure and overhead in parsing operations.
**Action:** When performing multiple single-byte or substring replacements, use a single-pass iteration with a `strings.Builder`. Add a `strings.ContainsAny` check as a fast path to avoid builder allocation altogether if the string is already clean.
