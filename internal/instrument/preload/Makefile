CC ?= gcc
CFLAGS = -Wall -Wextra -fPIC -shared -O2
SO = preload_shim.so

all: $(SO)

$(SO): csrc/shim.c
	$(CC) $(CFLAGS) -o $(SO) csrc/shim.c -ldl -lpthread

clean:
	rm -f $(SO)

.PHONY: all clean
