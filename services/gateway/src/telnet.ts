const IAC = 255
const DONT = 254
const DO = 253
const WONT = 252
const WILL = 251
const SB = 250
const SE = 240
const ECHO = 1

export type TelnetEvent =
  | { type: 'data'; data: Buffer }
  | { type: 'reply'; data: Buffer }
  | { type: 'echo'; enabled: boolean }

type ParserState = 'data' | 'iac' | 'option' | 'subnegotiation' | 'subnegotiation-iac'

/** Streaming Telnet filter. It never decodes application bytes. */
export class TelnetParser {
  private state: ParserState = 'data'
  private command = 0

  feed(chunk: Buffer): TelnetEvent[] {
    const events: TelnetEvent[] = []
    let data: number[] = []
    const flush = () => {
      if (data.length > 0) {
        events.push({ type: 'data', data: Buffer.from(data) })
        data = []
      }
    }

    for (const byte of chunk) {
      switch (this.state) {
        case 'data':
          if (byte === IAC) {
            flush()
            this.state = 'iac'
          } else {
            data.push(byte)
          }
          break
        case 'iac':
          if (byte === IAC) {
            data.push(IAC)
            this.state = 'data'
          } else if (byte === WILL || byte === WONT || byte === DO || byte === DONT) {
            this.command = byte
            this.state = 'option'
          } else if (byte === SB) {
            this.state = 'subnegotiation'
          } else {
            // One-byte Telnet commands (for example NOP) are intentionally consumed.
            this.state = 'data'
          }
          break
        case 'option':
          this.handleOption(this.command, byte, events)
          this.state = 'data'
          break
        case 'subnegotiation':
          if (byte === IAC) this.state = 'subnegotiation-iac'
          break
        case 'subnegotiation-iac':
          if (byte === SE) this.state = 'data'
          else if (byte !== IAC) this.state = 'subnegotiation'
          else this.state = 'subnegotiation'
          break
      }
    }
    flush()
    return events
  }

  private handleOption(command: number, option: number, events: TelnetEvent[]): void {
    if (option === ECHO && (command === WILL || command === WONT)) {
      events.push({ type: 'echo', enabled: command === WONT })
      events.push({ type: 'reply', data: Buffer.from([IAC, command === WILL ? DO : DONT, option]) })
      return
    }

    // The browser is not a Telnet client. Decline every other option deterministically.
    if (command === WILL) events.push({ type: 'reply', data: Buffer.from([IAC, DONT, option]) })
    if (command === DO) events.push({ type: 'reply', data: Buffer.from([IAC, WONT, option]) })
  }
}
