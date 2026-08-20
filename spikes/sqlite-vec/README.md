# sqlite-vec Go Spike

## Prerequisites
- Go 1.22+
- CGO toolchain (Xcode CLT on macOS, build-essential on Linux)

## Pre-built Extension
The `build/` directory contains pre-built sqlite-vec extensions downloaded from GitHub Releases v0.1.6:
- `build/vec.dylib` — macOS ARM64 (renamed from `vec0.dylib`)
- `build/sqlite-vec.so` — Linux x86_64 (not included, download separately)

## Run Spike

### Test mattn/go-sqlite3 (CGO)
```bash
cd spikes/sqlite-vec
CGO_ENABLED=1 go run main.go
```

### Test modernc.org/sqlite (pure Go)
```bash
cd spikes/sqlite-vec
CGO_ENABLED=0 go run main_modernc.go
```

## Expected Output

### mattn/go-sqlite3
```
=== mattn/go-sqlite3 (CGO) ===
Platform: darwin/arm64
Extension: build/vec.dylib
[mattn] ✅ sqlite-vec loaded
[mattn] Testing vec0...
[mattn] Creating vec0 table...
[mattn] Inserting vectors...
[mattn] Running KNN query...
[mattn] KNN Results:
  rowid=1 distance=0.100000
  rowid=2 distance=0.700000
  rowid=4 distance=1.004988
[mattn] ✅ vec0 KNN works
```

### modernc.org/sqlite
```
=== modernc.org/sqlite (pure Go) ===
Platform: darwin/arm64
Extension: build/sqlite-vec.dylib
[modernc] ⚠️  load_extension failed (expected): SQL logic error: not authorized
[modernc] modernc cannot load dynamic .so/.dylib extensions at runtime.
[modernc] Would require custom Wasm build with sqlite-vec baked in.
[modernc] FTS5 Results:
  rowid=1 rank=-0.000001
[modernc] ✅ FTS5 works (fallback)
```

## Gatekeeper Quarantine Fix (macOS)
If you get `dlopen() failed: code signature invalid`:
```bash
xattr -d com.apple.quarantine build/vec.dylib
```

## Key Findings
1. **mattn/go-sqlite3** works with sqlite-vec via `LoadExtension(lib, "sqlite3_vec_init")` on a dedicated connection
2. **modernc.org/sqlite** cannot load dynamic extensions at runtime (requires custom Wasm build)
3. **Extensions are per-connection** - all vec0 operations must use the same connection that loaded the extension
4. **vec0 KNN queries require `AND k = N`** parameter in the WHERE clause
5. **FTS5** works natively in modernc.org/sqlite; in mattn/go-sqlite3 requires `-tags sqlite_fts5`