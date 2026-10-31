import auth from '../locales/en/auth.json'
import board from '../locales/en/board.json'
import command from '../locales/en/command.json'
import common from '../locales/en/common.json'
import errors from '../locales/en/errors.json'
import issue from '../locales/en/issue.json'
import issues from '../locales/en/issues.json'
import projects from '../locales/en/projects.json'
import settings from '../locales/en/settings.json'

/** English catalogs, bundled; other languages will load on demand. */
export const en = { common, auth, settings, issues, board, issue, command, projects, errors }

export const namespaces = Object.keys(en) as (keyof typeof en)[]
