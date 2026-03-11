// SPDX-License-Identifier: AGPL-3.0-or-later

/** Error returned when the server replies with ok:false. */
export class SharkfinError extends Error {
  constructor(public readonly serverMessage: string) {
    super(`sharkfin: ${serverMessage}`);
    this.name = "SharkfinError";
  }
}
