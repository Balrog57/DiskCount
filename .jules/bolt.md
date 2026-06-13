## 2024-06-13 - Optimize NormalizeDriveCategory byte iteration
**Learning:** `asciiFold` combined with multiple `strings.ReplaceAll` calls creates unnecessary string allocations. The string replacement logic (`""` -> removed, `.` -> removed, `-` -> space) can be safely merged into a single-pass byte builder.
**Action:** Always prefer single-pass `strings.Builder` iterations for byte replacement / filtering logic in critical path parsing code over chained `strings.ReplaceAll` or pre-calls to `asciiFold` if the operations can be combined.
