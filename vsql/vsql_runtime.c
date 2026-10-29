// vsql_runtime.c compiles the garbage collector and V's helpers from the
// amalgamation, which have to be compiled separately from libvsql.c (see
// internal/amalgamation/vsql_runtime.c).
#include "../internal/amalgamation/vsql_runtime.c"

// vsql_gc_after_go_init is called by init (see libvsql.go), once the Go runtime
// has installed its signal handlers. libvsql initializes the garbage collector
// before that, from a C constructor, which has two problems:
//
// 1. On Linux, the garbage collector stops the other threads with signals
//    (SIGPWR and SIGXCPU), and Go replaces its handlers with its own. Go only
//    forwards synchronous signals to the handler it replaced, so the threads
//    never stop and the collector aborts ("Signals delivery fails
//    constantly"). GC_stop_init installs its handlers again.
//
// 2. The main thread is registered with the garbage collector for good, so the
//    collector could stop it, or scan its stack, while it's running Go code.
//    Every other thread is only registered while it's in a libvsql routine,
//    and after this the main thread is too.
void vsql_gc_after_go_init(void) {
#if defined(__linux__)
  GC_stop_init();
#endif
  if (GC_thread_is_registered()) {
    GC_unregister_my_thread();
  }
}
