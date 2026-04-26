package wstui

// viewport returns the [start, end) item indices that fit within budget
// while keeping cursor in view. offset is treated as a soft hint: it
// shrinks toward cursor when the cursor sits above it, and is pushed
// forward when the cursor would otherwise fall off the bottom.
//
// itemHeight reports how many lines item i renders to. Callers feed in
// whatever per-row height function makes sense (a single-line list
// passes func(int) int { return 1 }; a wrapping list inspects the
// rendered name).
func viewport(n, cursor, offset, budget int, itemHeight func(i int) int) (start, end int) {
	if n == 0 {
		return 0, 0
	}
	if budget <= 0 {
		return 0, n
	}

	start = offset
	if start < 0 {
		start = 0
	}
	if start > cursor {
		start = cursor
	}
	if start >= n {
		start = n - 1
	}

	for {
		used := 0
		end = start
		for end < n {
			h := itemHeight(end)
			if used+h > budget && used > 0 {
				break
			}
			used += h
			end++
		}
		if cursor < end || start >= n-1 {
			return start, end
		}
		start++
	}
}
