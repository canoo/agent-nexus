// Serializes asynchronous tasks in FIFO order. Errors are caught on the internal
// tail so subsequent tasks continue to run, while callers receive the original
// result promise and must handle any rejection.
export function createEventQueue() {
  let tail = Promise.resolve();

  function enqueue(task) {
    const result = tail.then(task);
    tail = result.catch(() => {});
    return result;
  }

  function idle() {
    return tail;
  }

  return { enqueue, idle };
}
