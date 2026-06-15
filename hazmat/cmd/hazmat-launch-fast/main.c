// Experimental lower-level launch helper for broker child profiling.
//
// This is intentionally not installed by default. It mirrors the critical
// hazmat-launch child semantics closely enough to benchmark whether a thin
// native helper materially reduces per-launch broker latency.

#include <ctype.h>
#include <dirent.h>
#include <errno.h>
#include <fcntl.h>
#include <stdarg.h>
#include <stdbool.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include <sys/types.h>
#include <unistd.h>

extern char **environ;
extern int sandbox_init(const char *profile, uint64_t flags, char **errorbuf);
extern void sandbox_free_error(char *errorbuf);

static const char policy_prefix[] = "/private/tmp/hazmat-";
static const char policy_suffix[] = ".sb";
static const char deny_default_marker[] = "(deny default)";
static const char session_temp_root[] = "/Users/agent/.cache/hazmat/tmp";

__attribute__((used)) static const char capability_markers[] =
    "--hazmat-direct-exec\0--hazmat-session-temp\0--hazmat-launch-profile";

struct launch_args {
    const char *metadata_json;
    bool direct_exec;
    const char *working_dir;
    const char *session_temp;
    const char **env_pairs;
    size_t env_count;
    char **cmd_argv;
};

__attribute__((noreturn)) static void die(const char *fmt, ...) {
    va_list ap;
    va_start(ap, fmt);
    vfprintf(stderr, fmt, ap);
    va_end(ap);
    fputc('\n', stderr);
    _exit(1);
}

static bool parse_int_fd(const char *s, int *out) {
    if (s == NULL || *s == '\0') {
        return false;
    }
    int value = 0;
    for (const unsigned char *p = (const unsigned char *)s; *p != '\0'; p++) {
        if (!isdigit(*p)) {
            return false;
        }
        value = value * 10 + (*p - '0');
    }
    *out = value;
    return true;
}

static void close_inherited_fds(void) {
    DIR *dir = opendir("/dev/fd");
    if (dir == NULL) {
        die("hazmat-launch-fast: read /dev/fd: %s", strerror(errno));
    }

    int dir_fd = dirfd(dir);
    size_t count = 0;
    size_t cap = 32;
    int *fds = calloc(cap, sizeof(int));
    if (fds == NULL) {
        closedir(dir);
        die("hazmat-launch-fast: allocate fd list");
    }

    struct dirent *entry;
    while ((entry = readdir(dir)) != NULL) {
        int fd = -1;
        if (!parse_int_fd(entry->d_name, &fd) || fd < 3 || fd == dir_fd) {
            continue;
        }
        if (count == cap) {
            cap *= 2;
            int *next = realloc(fds, cap * sizeof(int));
            if (next == NULL) {
                free(fds);
                closedir(dir);
                die("hazmat-launch-fast: grow fd list");
            }
            fds = next;
        }
        fds[count++] = fd;
    }
    closedir(dir);

    for (size_t i = 0; i < count; i++) {
        if (close(fds[i]) != 0 && errno != EBADF) {
            int err = errno;
            int fd = fds[i];
            free(fds);
            die("hazmat-launch-fast: close fd %d: %s", fd, strerror(err));
        }
    }
    free(fds);
}

static bool valid_policy_path(const char *path) {
    size_t prefix_len = strlen(policy_prefix);
    size_t suffix_len = strlen(policy_suffix);
    size_t len = strlen(path);
    if (len <= prefix_len + suffix_len) {
        return false;
    }
    if (strncmp(path, policy_prefix, prefix_len) != 0) {
        return false;
    }
    if (strcmp(path + len - suffix_len, policy_suffix) != 0) {
        return false;
    }
    for (const unsigned char *p = (const unsigned char *)path + prefix_len;
         p < (const unsigned char *)path + len - suffix_len; p++) {
        if (!isdigit(*p)) {
            return false;
        }
    }
    return true;
}

static char *read_all_cloexec(const char *path, size_t *len_out) {
    int fd = open(path, O_RDONLY | O_CLOEXEC);
    if (fd < 0) {
        die("hazmat-launch-fast: read %s: %s", path, strerror(errno));
    }
    struct stat st;
    if (fstat(fd, &st) != 0) {
        int err = errno;
        close(fd);
        die("hazmat-launch-fast: stat %s: %s", path, strerror(err));
    }
    if (st.st_size < 0 || st.st_size > 8 * 1024 * 1024) {
        close(fd);
        die("hazmat-launch-fast: policy file is too large");
    }

    size_t cap = (size_t)st.st_size + 1;
    char *buf = malloc(cap == 0 ? 1 : cap);
    if (buf == NULL) {
        close(fd);
        die("hazmat-launch-fast: allocate policy buffer");
    }

    size_t off = 0;
    while (off < cap - 1) {
        ssize_t n = read(fd, buf + off, cap - 1 - off);
        if (n < 0) {
            int err = errno;
            free(buf);
            close(fd);
            die("hazmat-launch-fast: read %s: %s", path, strerror(err));
        }
        if (n == 0) {
            break;
        }
        off += (size_t)n;
    }
    close(fd);
    buf[off] = '\0';
    *len_out = off;
    return buf;
}

static char *validate_and_read_policy(const char *path) {
    if (!valid_policy_path(path)) {
        die("hazmat-launch-fast: policy file must match /private/tmp/hazmat-<pid>.sb, got %s", path);
    }

    struct stat st;
    if (stat(path, &st) != 0) {
        die("hazmat-launch-fast: stat %s: %s", path, strerror(errno));
    }

    const char *sudo_uid_s = getenv("SUDO_UID");
    if (sudo_uid_s == NULL || *sudo_uid_s == '\0') {
        die("hazmat-launch-fast: SUDO_UID not set or root - not a valid sudo invocation");
    }
    char *end = NULL;
    errno = 0;
    long sudo_uid = strtol(sudo_uid_s, &end, 10);
    if (errno != 0 || end == sudo_uid_s || *end != '\0' || sudo_uid == 0) {
        die("hazmat-launch-fast: SUDO_UID not set or root - not a valid sudo invocation");
    }
    if ((long)st.st_uid != sudo_uid) {
        die("hazmat-launch-fast: policy file owner uid %d does not match invoking user uid %ld",
            (int)st.st_uid, sudo_uid);
    }
    if ((st.st_mode & 0777) != 0644) {
        die("hazmat-launch-fast: policy file has mode %04o, expected 0644", st.st_mode & 0777);
    }

    size_t policy_len = 0;
    char *policy = read_all_cloexec(path, &policy_len);
    (void)policy_len;
    if (strstr(policy, deny_default_marker) == NULL) {
        free(policy);
        die("hazmat-launch-fast: policy file does not contain %s - permissive policies are not allowed",
            deny_default_marker);
    }
    return policy;
}

static bool is_leaf_session_temp_name(const char *leaf) {
    bool saw_dash = false;
    bool saw_digit_before = false;
    bool saw_digit_after = false;
    for (const unsigned char *p = (const unsigned char *)leaf; *p != '\0'; p++) {
        if (*p == '-') {
            if (saw_dash || !saw_digit_before) {
                return false;
            }
            saw_dash = true;
            continue;
        }
        if (!isdigit(*p)) {
            return false;
        }
        if (saw_dash) {
            saw_digit_after = true;
        } else {
            saw_digit_before = true;
        }
    }
    return saw_dash && saw_digit_before && saw_digit_after;
}

static void ensure_dir(const char *path, mode_t mode) {
    char tmp[4096];
    size_t len = strlen(path);
    if (len == 0 || len >= sizeof(tmp)) {
        die("hazmat-launch-fast: invalid directory path");
    }
    memcpy(tmp, path, len + 1);
    for (char *p = tmp + 1; *p != '\0'; p++) {
        if (*p != '/') {
            continue;
        }
        *p = '\0';
        if (mkdir(tmp, mode) != 0 && errno != EEXIST) {
            die("hazmat-launch-fast: create %s: %s", tmp, strerror(errno));
        }
        *p = '/';
    }
    if (mkdir(tmp, mode) != 0 && errno != EEXIST) {
        die("hazmat-launch-fast: create %s: %s", tmp, strerror(errno));
    }
}

static void prepare_session_temp_dir(const char *path) {
    size_t root_len = strlen(session_temp_root);
    if (strncmp(path, session_temp_root, root_len) != 0 || path[root_len] != '/') {
        die("hazmat-launch-fast: --hazmat-session-temp path %s must be a direct child of %s",
            path, session_temp_root);
    }
    const char *leaf = path + root_len + 1;
    if (*leaf == '\0' || strchr(leaf, '/') != NULL || !is_leaf_session_temp_name(leaf)) {
        die("hazmat-launch-fast: --hazmat-session-temp path %s has invalid generated name", path);
    }
    ensure_dir(session_temp_root, 0700);
    if (mkdir(path, 0700) != 0) {
        die("hazmat-launch-fast: create session temp dir: %s", strerror(errno));
    }
    if (chmod(path, 0700) != 0) {
        die("hazmat-launch-fast: set session temp mode: %s", strerror(errno));
    }
}

__attribute__((noreturn)) static void usage(void) {
    die("usage: hazmat-launch-fast [--hazmat-launch-profile] <policy-file> <cmd> [args...]\n"
        "       hazmat-launch-fast [--hazmat-launch-profile] exec <cmd> [args...]");
}

static struct launch_args parse_launch_args(int argc, char **argv, int start) {
    struct launch_args parsed = {0};
    parsed.env_pairs = calloc((size_t)argc, sizeof(char *));
    if (parsed.env_pairs == NULL) {
        die("hazmat-launch-fast: allocate env pairs");
    }

    int i = start;
    while (i < argc) {
        const char *arg = argv[i];
        if (strcmp(arg, "--hazmat-metadata-json") == 0) {
            if (i + 2 >= argc || argv[i + 1][0] == '\0') {
                die("hazmat-launch-fast: --hazmat-metadata-json requires a JSON payload and command");
            }
            parsed.metadata_json = argv[i + 1];
            i += 2;
        } else if (strcmp(arg, "--hazmat-session-temp") == 0) {
            if (i + 1 >= argc || argv[i + 1][0] == '\0') {
                die("hazmat-launch-fast: --hazmat-session-temp requires a path");
            }
            parsed.session_temp = argv[i + 1];
            i += 2;
        } else if (strcmp(arg, "--hazmat-direct-exec") == 0) {
            parsed.direct_exec = true;
            i += 1;
        } else if (strcmp(arg, "--hazmat-working-dir") == 0) {
            if (i + 1 >= argc || argv[i + 1][0] == '\0') {
                die("hazmat-launch-fast: --hazmat-working-dir requires a path");
            }
            parsed.working_dir = argv[i + 1];
            i += 2;
        } else if (strcmp(arg, "--hazmat-env") == 0) {
            if (i + 1 >= argc || argv[i + 1][0] == '\0' || strchr(argv[i + 1], '=') == NULL) {
                die("hazmat-launch-fast: --hazmat-env requires KEY=VALUE");
            }
            parsed.env_pairs[parsed.env_count++] = argv[i + 1];
            i += 2;
        } else if (strcmp(arg, "--") == 0) {
            i += 1;
            parsed.cmd_argv = &argv[i];
            break;
        } else {
            parsed.cmd_argv = &argv[i];
            break;
        }
    }
    if (parsed.cmd_argv == NULL || parsed.cmd_argv[0] == NULL) {
        die("hazmat-launch-fast: missing command");
    }
    if (parsed.direct_exec && (parsed.working_dir == NULL || parsed.working_dir[0] == '\0')) {
        die("hazmat-launch-fast: --hazmat-direct-exec requires --hazmat-working-dir");
    }
    return parsed;
}

static char *resolve_exec_path(const char *cmd) {
    if (cmd == NULL || cmd[0] == '\0') {
        die("hazmat-launch-fast: missing command");
    }
    if (cmd[0] == '/') {
        return strdup(cmd);
    }
    if (strchr(cmd, '/') != NULL) {
        char cwd[4096];
        if (getcwd(cwd, sizeof(cwd)) == NULL) {
            die("hazmat-launch-fast: getcwd: %s", strerror(errno));
        }
        size_t len = strlen(cwd) + 1 + strlen(cmd) + 1;
        char *full = malloc(len);
        if (full == NULL) {
            die("hazmat-launch-fast: allocate command path");
        }
        snprintf(full, len, "%s/%s", cwd, cmd);
        if (access(full, X_OK) == 0) {
            return full;
        }
        free(full);
        die("hazmat-launch-fast: command not found: %s", cmd);
    }

    const char *path = getenv("PATH");
    if (path == NULL) {
        die("hazmat-launch-fast: command not found: %s", cmd);
    }
    const char *start = path;
    while (*start != '\0') {
        const char *end = strchr(start, ':');
        size_t dir_len = end == NULL ? strlen(start) : (size_t)(end - start);
        if (dir_len > 0) {
            size_t len = dir_len + 1 + strlen(cmd) + 1;
            char *full = malloc(len);
            if (full == NULL) {
                die("hazmat-launch-fast: allocate command path");
            }
            memcpy(full, start, dir_len);
            full[dir_len] = '/';
            strcpy(full + dir_len + 1, cmd);
            if (access(full, X_OK) == 0) {
                return full;
            }
            free(full);
        }
        if (end == NULL) {
            break;
        }
        start = end + 1;
    }
    die("hazmat-launch-fast: command not found: %s", cmd);
}

static void set_direct_env(const struct launch_args *args) {
    static char *empty_env[] = {NULL};
    environ = empty_env;
    for (size_t i = 0; i < args->env_count; i++) {
        const char *pair = args->env_pairs[i];
        const char *eq = strchr(pair, '=');
        if (eq == NULL || eq == pair) {
            die("hazmat-launch-fast: invalid --hazmat-env value %s", pair);
        }
        size_t key_len = (size_t)(eq - pair);
        char *key = strndup(pair, key_len);
        if (key == NULL) {
            die("hazmat-launch-fast: allocate env key");
        }
        if (setenv(key, eq + 1, 1) != 0) {
            die("hazmat-launch-fast: set %s: %s", key, strerror(errno));
        }
        free(key);
    }
}

__attribute__((noreturn)) static void exec_command(char **cmd_argv) {
    char *bin = resolve_exec_path(cmd_argv[0]);
    execve(bin, cmd_argv, environ);
    die("hazmat-launch-fast: exec %s: %s", bin, strerror(errno));
}

int main(int argc, char **argv) {
    close_inherited_fds();

    int i = 1;
    if (i < argc && strcmp(argv[i], "--hazmat-launch-profile") == 0) {
        i++;
    }
    if (i >= argc) {
        usage();
    }

    if (strcmp(argv[i], "exec") == 0) {
        if (i + 1 >= argc) {
            usage();
        }
        exec_command(&argv[i + 1]);
    }

    const char *policy_path = argv[i++];
    if (i >= argc) {
        usage();
    }
    struct launch_args args = parse_launch_args(argc, argv, i);
    char *policy = validate_and_read_policy(policy_path);

    if (args.session_temp != NULL) {
        prepare_session_temp_dir(args.session_temp);
    }

    char *sandbox_error = NULL;
    if (sandbox_init(policy, 0, &sandbox_error) != 0) {
        const char *msg = sandbox_error == NULL ? "unknown error" : sandbox_error;
        die("hazmat-launch-fast: sandbox_init: %s", msg);
    }
    if (sandbox_error != NULL) {
        sandbox_free_error(sandbox_error);
    }
    free(policy);

    if (args.metadata_json != NULL) {
        fprintf(stderr, "%s\n", args.metadata_json);
        fflush(stderr);
    }

    if (args.direct_exec) {
        set_direct_env(&args);
        if (chdir(args.working_dir) != 0) {
            die("hazmat-launch-fast: chdir %s: %s", args.working_dir, strerror(errno));
        }
    }
    exec_command(args.cmd_argv);
}
