// SPDX-License-Identifier: Apache-2.0

type Listener = (...args: unknown[]) => void;

export class Emitter {
  private listeners = new Map<string, Set<Listener>>();

  on(event: string, fn: Listener): this {
    if (!this.listeners.has(event)) {
      this.listeners.set(event, new Set());
    }
    this.listeners.get(event)!.add(fn);
    return this;
  }

  off(event: string, fn: Listener): this {
    this.listeners.get(event)?.delete(fn);
    return this;
  }

  protected emit(event: string, ...args: unknown[]): void {
    this.listeners.get(event)?.forEach((fn) => fn(...args));
  }

  removeAllListeners(): void {
    this.listeners.clear();
  }
}
