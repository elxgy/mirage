#define _GNU_SOURCE
#include <dlfcn.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
#include <stdint.h>
#include <stdio.h>
#include <pthread.h>

static pthread_mutex_t write_mutex = PTHREAD_MUTEX_INITIALIZER;
static int64_t count_malloc, count_free, count_read, count_write;
static char out_path[1024];
static int out_path_set;

static void write_counts(void) {
    if (!out_path_set || out_path[0] == '\0') return;
    FILE *f = fopen(out_path, "w");
    if (!f) return;
    pthread_mutex_lock(&write_mutex);
    int64_t m = __atomic_load_n(&count_malloc, __ATOMIC_RELAXED);
    int64_t fv = __atomic_load_n(&count_free, __ATOMIC_RELAXED);
    int64_t r = __atomic_load_n(&count_read, __ATOMIC_RELAXED);
    int64_t w = __atomic_load_n(&count_write, __ATOMIC_RELAXED);
    fprintf(f, "malloc %ld\n", (long)m);
    fprintf(f, "free %ld\n", (long)fv);
    fprintf(f, "read %ld\n", (long)r);
    fprintf(f, "write %ld\n", (long)w);
    pthread_mutex_unlock(&write_mutex);
    fclose(f);
}

static void init_out_path(void) {
    const char *env = getenv("MIRAGE_PRELOAD_OUT");
    if (env && strlen(env) < sizeof(out_path)) {
        strncpy(out_path, env, sizeof(out_path) - 1);
        out_path[sizeof(out_path)-1] = '\0';
        out_path_set = 1;
        atexit(write_counts);
    }
}

typedef void *(*malloc_fn)(size_t);
typedef void (*free_fn)(void*);
typedef ssize_t (*read_fn)(int, void*, size_t);
typedef ssize_t (*write_fn)(int, const void*, size_t);

void *malloc(size_t size) {
    static malloc_fn real;
    if (!real) real = (malloc_fn)dlsym(RTLD_NEXT, "malloc");
    __atomic_fetch_add(&count_malloc, 1, __ATOMIC_RELAXED);
    return real ? real(size) : NULL;
}

void free(void *ptr) {
    static free_fn real;
    if (!real) real = (free_fn)dlsym(RTLD_NEXT, "free");
    __atomic_fetch_add(&count_free, 1, __ATOMIC_RELAXED);
    if (real) real(ptr);
}

ssize_t read(int fd, void *buf, size_t count) {
    static read_fn real;
    if (!real) real = (read_fn)dlsym(RTLD_NEXT, "read");
    __atomic_fetch_add(&count_read, 1, __ATOMIC_RELAXED);
    return real ? real(fd, buf, count) : -1;
}

ssize_t write(int fd, const void *buf, size_t count) {
    static write_fn real;
    if (!real) real = (write_fn)dlsym(RTLD_NEXT, "write");
    __atomic_fetch_add(&count_write, 1, __ATOMIC_RELAXED);
    return real ? real(fd, buf, count) : -1;
}

__attribute__((constructor))
static void preload_init(void) {
    init_out_path();
}
