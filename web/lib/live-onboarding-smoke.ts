/**
 * Test-only guard for the operator-run live onboarding smoke. It is purposely
 * separate from browser configuration: ordinary unit/Playwright commands must
 * neither launch a browser nor contact a deployed service.
 *
 * A runnable smoke consumes only explicitly named, pre-created fixtures. It
 * can create durable provisioning/claim state only after an operator supplies
 * the independent approval value below for the approved maintenance window.
 */
export const LIVE_ONBOARDING_SMOKE_APPROVAL = 'approved-durable-onboarding-data'
export const LIVE_ONBOARDING_SMOKE_GLOBAL_UNIQUENESS_APPROVAL =
  'confirmed-global-uniqueness'

export interface LiveOnboardingSmokeEnvironment {
  MUHAN_LIVE_ONBOARDING_SMOKE_ENABLED?: string
  MUHAN_LIVE_ONBOARDING_SMOKE_DURABLE_DATA_APPROVAL?: string
  MUHAN_LIVE_ONBOARDING_SMOKE_GLOBAL_UNIQUENESS_APPROVAL?: string
  MUHAN_LIVE_ONBOARDING_SMOKE_BASE_URL?: string
  MUHAN_LIVE_ONBOARDING_SMOKE_PROVISION_EMAIL?: string
  MUHAN_LIVE_ONBOARDING_SMOKE_PROVISION_WEB_PASSWORD?: string
  MUHAN_LIVE_ONBOARDING_SMOKE_PROVISION_CHARACTER_NAME?: string
  MUHAN_LIVE_ONBOARDING_SMOKE_PROVISION_GENDER?: string
  MUHAN_LIVE_ONBOARDING_SMOKE_PROVISION_CLASS?: string
  MUHAN_LIVE_ONBOARDING_SMOKE_PROVISION_STATS?: string
  MUHAN_LIVE_ONBOARDING_SMOKE_PROVISION_WEAPON?: string
  MUHAN_LIVE_ONBOARDING_SMOKE_PROVISION_ALIGNMENT?: string
  MUHAN_LIVE_ONBOARDING_SMOKE_PROVISION_RACE?: string
  MUHAN_LIVE_ONBOARDING_SMOKE_PROVISION_GAME_PASSWORD?: string
  MUHAN_LIVE_ONBOARDING_SMOKE_CLAIM_EMAIL?: string
  MUHAN_LIVE_ONBOARDING_SMOKE_CLAIM_WEB_PASSWORD?: string
  MUHAN_LIVE_ONBOARDING_SMOKE_CLAIM_CHARACTER_NAME?: string
  MUHAN_LIVE_ONBOARDING_SMOKE_CLAIM_LEGACY_PASSWORD?: string
}

export interface LiveOnboardingSmokeConfig {
  baseUrl: string
  provision: {
    email: string
    webPassword: string
    characterName: string
    gender: string
    characterClass: string
    stats: string
    weapon: string
    alignment: string
    race: string
    gamePassword: string
  }
  claim: {
    email: string
    webPassword: string
    characterName: string
    legacyPassword: string
  }
}

export interface LiveOnboardingSmokeConfigResult {
  config: LiveOnboardingSmokeConfig | null
  invalid: string[]
  missing: string[]
  shouldRun: boolean
  skipReason: string
}

const fixtureKeys = [
  'MUHAN_LIVE_ONBOARDING_SMOKE_BASE_URL',
  'MUHAN_LIVE_ONBOARDING_SMOKE_PROVISION_EMAIL',
  'MUHAN_LIVE_ONBOARDING_SMOKE_PROVISION_WEB_PASSWORD',
  'MUHAN_LIVE_ONBOARDING_SMOKE_PROVISION_CHARACTER_NAME',
  'MUHAN_LIVE_ONBOARDING_SMOKE_PROVISION_GENDER',
  'MUHAN_LIVE_ONBOARDING_SMOKE_PROVISION_CLASS',
  'MUHAN_LIVE_ONBOARDING_SMOKE_PROVISION_STATS',
  'MUHAN_LIVE_ONBOARDING_SMOKE_PROVISION_WEAPON',
  'MUHAN_LIVE_ONBOARDING_SMOKE_PROVISION_ALIGNMENT',
  'MUHAN_LIVE_ONBOARDING_SMOKE_PROVISION_RACE',
  'MUHAN_LIVE_ONBOARDING_SMOKE_PROVISION_GAME_PASSWORD',
  'MUHAN_LIVE_ONBOARDING_SMOKE_CLAIM_EMAIL',
  'MUHAN_LIVE_ONBOARDING_SMOKE_CLAIM_WEB_PASSWORD',
  'MUHAN_LIVE_ONBOARDING_SMOKE_CLAIM_CHARACTER_NAME',
  'MUHAN_LIVE_ONBOARDING_SMOKE_CLAIM_LEGACY_PASSWORD',
] as const

function isPresent(value: string | undefined): value is string {
  return typeof value === 'string' && value.trim().length > 0
}

function isHttpsUrl(value: string): boolean {
  try {
    const url = new URL(value)
    return url.protocol === 'https:' && !url.username && !url.password
  } catch {
    return false
  }
}

function isPlaceholder(value: string): boolean {
  return (
    /^(?:<[^>]+>|\$\{[^}]+\}|\[[^\]]+\]|change[-_ ]?me|replace[-_ ]?me|placeholder|todo|tbd)$/iu.test(value)
  )
}

function isSafeFixtureValue(value: string | undefined): value is string {
  return (
    isPresent(value) &&
    value === value.trim() &&
    !/[\u0000-\u001f\u007f]/u.test(value) &&
    !isPlaceholder(value)
  )
}

function isFixtureEmail(value: string | undefined): value is string {
  return (
    isSafeFixtureValue(value) &&
    value.length <= 254 &&
    /^[^\s@]+@[^\s@]+\.[^\s@]+$/u.test(value)
  )
}

function isFixtureCharacterName(value: string | undefined): value is string {
  if (!isSafeFixtureValue(value)) return false
  return (
    value !== '.' &&
    value !== '..' &&
    [...value].length <= 12 &&
    new TextEncoder().encode(value).byteLength <= 14 &&
    !/[\\/:]/u.test(value) &&
    canonicalFixtureName(value) === value
  )
}

function canonicalFixtureName(value: string): string {
  let canonical = value.normalize('NFC').replace(/[A-Z]/g, (letter) => letter.toLowerCase())
  if (/^[a-z]/.test(canonical)) {
    canonical = canonical[0]!.toUpperCase() + canonical.slice(1)
  }
  return canonical
}

function normalizedEmail(value: string): string {
  return value.normalize('NFC').toLowerCase()
}

function invalidFixtureKeys(environment: LiveOnboardingSmokeEnvironment): string[] {
  const invalid: string[] = []
  const check = (key: keyof LiveOnboardingSmokeEnvironment, valid: boolean) => {
    if (!valid) invalid.push(key)
  }

  check('MUHAN_LIVE_ONBOARDING_SMOKE_PROVISION_EMAIL', isFixtureEmail(environment.MUHAN_LIVE_ONBOARDING_SMOKE_PROVISION_EMAIL))
  check('MUHAN_LIVE_ONBOARDING_SMOKE_PROVISION_WEB_PASSWORD', isSafeFixtureValue(environment.MUHAN_LIVE_ONBOARDING_SMOKE_PROVISION_WEB_PASSWORD))
  check('MUHAN_LIVE_ONBOARDING_SMOKE_PROVISION_CHARACTER_NAME', isFixtureCharacterName(environment.MUHAN_LIVE_ONBOARDING_SMOKE_PROVISION_CHARACTER_NAME))
  check('MUHAN_LIVE_ONBOARDING_SMOKE_PROVISION_GENDER', isSafeFixtureValue(environment.MUHAN_LIVE_ONBOARDING_SMOKE_PROVISION_GENDER))
  check('MUHAN_LIVE_ONBOARDING_SMOKE_PROVISION_CLASS', isSafeFixtureValue(environment.MUHAN_LIVE_ONBOARDING_SMOKE_PROVISION_CLASS))
  check('MUHAN_LIVE_ONBOARDING_SMOKE_PROVISION_STATS', isSafeFixtureValue(environment.MUHAN_LIVE_ONBOARDING_SMOKE_PROVISION_STATS))
  check('MUHAN_LIVE_ONBOARDING_SMOKE_PROVISION_WEAPON', isSafeFixtureValue(environment.MUHAN_LIVE_ONBOARDING_SMOKE_PROVISION_WEAPON))
  check('MUHAN_LIVE_ONBOARDING_SMOKE_PROVISION_ALIGNMENT', isSafeFixtureValue(environment.MUHAN_LIVE_ONBOARDING_SMOKE_PROVISION_ALIGNMENT))
  check('MUHAN_LIVE_ONBOARDING_SMOKE_PROVISION_RACE', isSafeFixtureValue(environment.MUHAN_LIVE_ONBOARDING_SMOKE_PROVISION_RACE))
  check('MUHAN_LIVE_ONBOARDING_SMOKE_PROVISION_GAME_PASSWORD', isSafeFixtureValue(environment.MUHAN_LIVE_ONBOARDING_SMOKE_PROVISION_GAME_PASSWORD))
  check('MUHAN_LIVE_ONBOARDING_SMOKE_CLAIM_EMAIL', isFixtureEmail(environment.MUHAN_LIVE_ONBOARDING_SMOKE_CLAIM_EMAIL))
  check('MUHAN_LIVE_ONBOARDING_SMOKE_CLAIM_WEB_PASSWORD', isSafeFixtureValue(environment.MUHAN_LIVE_ONBOARDING_SMOKE_CLAIM_WEB_PASSWORD))
  check('MUHAN_LIVE_ONBOARDING_SMOKE_CLAIM_CHARACTER_NAME', isFixtureCharacterName(environment.MUHAN_LIVE_ONBOARDING_SMOKE_CLAIM_CHARACTER_NAME))
  check('MUHAN_LIVE_ONBOARDING_SMOKE_CLAIM_LEGACY_PASSWORD', isSafeFixtureValue(environment.MUHAN_LIVE_ONBOARDING_SMOKE_CLAIM_LEGACY_PASSWORD))
  return invalid
}

export function readLiveOnboardingSmokeConfig(
  environment: LiveOnboardingSmokeEnvironment = process.env as LiveOnboardingSmokeEnvironment,
): LiveOnboardingSmokeConfigResult {
  if (environment.MUHAN_LIVE_ONBOARDING_SMOKE_ENABLED !== 'true') {
    return {
      config: null,
      invalid: [],
      missing: [],
      shouldRun: false,
      skipReason: 'Live onboarding smoke is disabled; set MUHAN_LIVE_ONBOARDING_SMOKE_ENABLED=true only for an approved operator run.',
    }
  }

  const missing = fixtureKeys.filter((key) => !isPresent(environment[key]))
  if (missing.length > 0) {
    return {
      config: null,
      invalid: [],
      missing: [...missing],
      shouldRun: false,
      skipReason: 'Live onboarding smoke requires every explicitly supplied pre-created fixture input.',
    }
  }

  const baseUrl = environment.MUHAN_LIVE_ONBOARDING_SMOKE_BASE_URL!
  if (!isHttpsUrl(baseUrl)) {
    return {
      config: null,
      invalid: [],
      missing: [],
      shouldRun: false,
      skipReason: 'Live onboarding smoke requires an HTTPS base URL without embedded credentials.',
    }
  }

  if (
    environment.MUHAN_LIVE_ONBOARDING_SMOKE_DURABLE_DATA_APPROVAL !==
    LIVE_ONBOARDING_SMOKE_APPROVAL
  ) {
    return {
      config: null,
      invalid: [],
      missing: [],
      shouldRun: false,
      skipReason: 'Live onboarding smoke needs separate operator approval before it can create durable onboarding or claim data.',
    }
  }

  if (
    environment.MUHAN_LIVE_ONBOARDING_SMOKE_GLOBAL_UNIQUENESS_APPROVAL !==
    LIVE_ONBOARDING_SMOKE_GLOBAL_UNIQUENESS_APPROVAL
  ) {
    return {
      config: null,
      invalid: [],
      missing: [],
      shouldRun: false,
      skipReason: 'Live onboarding smoke requires an operator attestation that global fixture uniqueness was checked before the run.',
    }
  }

  const invalid = invalidFixtureKeys(environment)
  if (invalid.length > 0) {
    return {
      config: null,
      invalid,
      missing: [],
      shouldRun: false,
      skipReason: 'Live onboarding smoke rejected an invalid fixture value; fixture values are never included in the skip reason.',
    }
  }

  if (
    normalizedEmail(environment.MUHAN_LIVE_ONBOARDING_SMOKE_PROVISION_EMAIL!) ===
    normalizedEmail(environment.MUHAN_LIVE_ONBOARDING_SMOKE_CLAIM_EMAIL!)
  ) {
    return {
      config: null,
      invalid: ['MUHAN_LIVE_ONBOARDING_SMOKE_CLAIM_EMAIL'],
      missing: [],
      shouldRun: false,
      skipReason: 'Live onboarding smoke requires provision and claim fixtures to use different web accounts.',
    }
  }

  if (
    canonicalFixtureName(environment.MUHAN_LIVE_ONBOARDING_SMOKE_PROVISION_CHARACTER_NAME!) ===
    canonicalFixtureName(environment.MUHAN_LIVE_ONBOARDING_SMOKE_CLAIM_CHARACTER_NAME!)
  ) {
    return {
      config: null,
      invalid: ['MUHAN_LIVE_ONBOARDING_SMOKE_CLAIM_CHARACTER_NAME'],
      missing: [],
      shouldRun: false,
      skipReason: 'Live onboarding smoke requires provision and claim fixtures to use different character names.',
    }
  }

  return {
    config: {
      baseUrl: baseUrl.replace(/\/$/, ''),
      provision: {
        email: environment.MUHAN_LIVE_ONBOARDING_SMOKE_PROVISION_EMAIL!,
        webPassword: environment.MUHAN_LIVE_ONBOARDING_SMOKE_PROVISION_WEB_PASSWORD!,
        characterName: environment.MUHAN_LIVE_ONBOARDING_SMOKE_PROVISION_CHARACTER_NAME!,
        gender: environment.MUHAN_LIVE_ONBOARDING_SMOKE_PROVISION_GENDER!,
        characterClass: environment.MUHAN_LIVE_ONBOARDING_SMOKE_PROVISION_CLASS!,
        stats: environment.MUHAN_LIVE_ONBOARDING_SMOKE_PROVISION_STATS!,
        weapon: environment.MUHAN_LIVE_ONBOARDING_SMOKE_PROVISION_WEAPON!,
        alignment: environment.MUHAN_LIVE_ONBOARDING_SMOKE_PROVISION_ALIGNMENT!,
        race: environment.MUHAN_LIVE_ONBOARDING_SMOKE_PROVISION_RACE!,
        gamePassword: environment.MUHAN_LIVE_ONBOARDING_SMOKE_PROVISION_GAME_PASSWORD!,
      },
      claim: {
        email: environment.MUHAN_LIVE_ONBOARDING_SMOKE_CLAIM_EMAIL!,
        webPassword: environment.MUHAN_LIVE_ONBOARDING_SMOKE_CLAIM_WEB_PASSWORD!,
        characterName: environment.MUHAN_LIVE_ONBOARDING_SMOKE_CLAIM_CHARACTER_NAME!,
        legacyPassword: environment.MUHAN_LIVE_ONBOARDING_SMOKE_CLAIM_LEGACY_PASSWORD!,
      },
    },
    invalid: [],
    missing: [],
    shouldRun: true,
    skipReason: '',
  }
}
