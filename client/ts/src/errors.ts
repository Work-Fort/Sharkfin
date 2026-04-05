// SPDX-License-Identifier: Apache-2.0

/** Error returned when the server replies with ok:false. */
export class SharkfinError extends Error {
  constructor(public readonly serverMessage: string) {
    super(`sharkfin: ${serverMessage}`);
    this.name = "SharkfinError";
  }
}
