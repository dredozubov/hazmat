# Print the JSON object assigned to the top-level key passed as -v key=<name>.
# The input is expected to be pretty-printed JSON with the key/object opening on
# one line, as produced by Hazmat's CLI JSON renderers.
BEGIN {
	if (key == "") {
		exit 2
	}
}

{
	line = $0
	if (!capturing) {
		pattern = "\"" key "\"[[:space:]]*:[[:space:]]*\\{"
		if (match(line, pattern) == 0) {
			next
		}
		capturing = 1
		found = 1
	}

	print line

	for (i = 1; i <= length(line); i++) {
		ch = substr(line, i, 1)
		if (escaped) {
			escaped = 0
			continue
		}
		if (in_string) {
			if (ch == "\\") {
				escaped = 1
			} else if (ch == "\"") {
				in_string = 0
			}
			continue
		}
		if (ch == "\"") {
			in_string = 1
		} else if (ch == "{") {
			depth++
		} else if (ch == "}") {
			depth--
			if (depth == 0) {
				done = 1
				exit
			}
		}
	}
}

END {
	if (!found || !done) {
		exit 1
	}
}
