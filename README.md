# go-logger - Level based logger with sub-logger support

## What is it?
Borrowed from golang stdlib, this enables logging at increasing
levels of verbosity. The verbosity increases as we go down the list
below:

    - Emergency (LOG_EMERG) - will halt the program after
      printing a backtrace of the calling goroutine.
    - Critical (LOG_CRIT)
    - Error (LOG_ERR) - all levels at and above will print a stack-trace
      of the calling goroutine.
    - Warning (LOG_WARNING)
    - Informational (LOG_INFO) - this is the level at which I log
      most informational messages useful for troubleshooting
      production issues.
    - Debug (LOG_DEBUG) - this is the most verbose level


## List of enhancements from the stdlib
- All I/O is done asychronously; the caller doesn't incur I/O cost

- A single program can have multiple loggers - each with a different
  priority.

- An instance of a logger is configured with a given log level;
  and it only prints log messages "above" the configured level.
  e.g., if a logger is configured with level of INFO, then it will
  print all log messages with INFO and higher priority;
  in particular, it won't print DEBUG messages.

- A single program can have multiple loggers; each with a
  different priority.

- The logger method Backtrace() will print a stack backtrace to
  the configured output stream. Log levels are NOT
  considered when backtraces are printed.

- The Panic() and Fatal() logger methods implicitly print the
  stack backtrace (upto 5 levels).

- DEBUG, ERR, CRIT log outputs (via Debug(), Err() and Crit()
  methods) also print the source file location from whence they
  were invoked.

- New package functions to create a syslog(1) or a file logger
  instance.

- Callers can create a new logger instance if they have an
  io.writer instance of their own - in case the existing output
  streams (File and Syslog) are insufficient.

- Any logger instance can create child-loggers with a different
  priority and prefix (but same destination); this is useful in large
  programs with different modules.

- Compressed log rotation based on daily time-of-day (configurable ToD) -- only
  available for file-backed destinations.

- Wrapper available to make this logger appear like a stdlib logger;
  this wrapper prints everything sent to it (it's an io.Writer)

