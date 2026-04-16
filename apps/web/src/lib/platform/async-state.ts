export type AsyncState<T, E = string> =
  | {
      status: "idle";
    }
  | {
      status: "loading";
    }
  | {
      status: "success";
      data: T;
    }
  | {
      status: "error";
      error: E;
    };

export function idleState<T, E = string>(): AsyncState<T, E> {
  return {
    status: "idle"
  };
}

export function loadingState<T, E = string>(): AsyncState<T, E> {
  return {
    status: "loading"
  };
}

export function successState<T, E = string>(data: T): AsyncState<T, E> {
  return {
    status: "success",
    data
  };
}

export function errorState<T, E = string>(error: E): AsyncState<T, E> {
  return {
    status: "error",
    error
  };
}
