import type { Content, ContentField, Issue } from '../types/campaign'
import { render, runeLength } from './rules'

export const LIMITS: Record<ContentField, number> = { sms_text: 160, email_subject: 80, email_body: 5000 }
const ALLOWED = ['{{first_name}}', '{{city}}']
// Backend'deki suffixRe ile aynı: placeholder'a yapışık ek ("{{city}}'da").
const SUFFIX = /\{\{(first_name|city)\}\}['’]?\p{L}+/u

// localIssues backend'deki content.Validate'in ANLIK geri bildirim için
// sadeleştirilmiş aynası. Otorite backend'dir; bu yalnızca kullanıcıyı
// kaydetmeden önce uyarır ve butonları kapatır.
export function localIssues(c: Content, vars: { first_name: string; city: string } | null): Issue[] {
  const out: Issue[] = []
  for (const field of Object.keys(LIMITS) as ContentField[]) {
    const tpl = c[field].trim()
    if (!tpl) {
      out.push({ path: field, code: 'required', message: 'boş olamaz' })
      continue
    }
    let rest = tpl
    for (const p of ALLOWED) rest = rest.split(p).join('')
    const bad = rest.match(/\{\{[^}]*\}\}/g)
    if (bad) out.push({ path: field, code: 'unknown_placeholder', message: `bilinmeyen placeholder ${bad[0]}; yalnızca ${ALLOWED.join(', ')}` })
    else if (rest.includes('{{') || rest.includes('}}')) out.push({ path: field, code: 'broken_placeholder', message: 'bozuk placeholder' })
    const suffix = tpl.match(SUFFIX)
    if (suffix) {
      out.push({
        path: field,
        code: 'placeholder_suffix',
        message: `placeholder'a ek getirilmiş (${suffix[0]}): ek şehre/ada göre değişir, cümleyi eksiz kurun (ör. "{{city}} için")`,
      })
    }
    const len = runeLength(vars ? render(tpl, vars) : tpl)
    if (len > LIMITS[field]) {
      out.push({ path: field, code: 'too_long', message: `${len} karakter${vars ? ' (en uzun adla)' : ''}, en fazla ${LIMITS[field]}` })
    }
  }
  if (/[\r\n]/.test(c.email_subject.trim())) out.push({ path: 'email_subject', code: 'newline', message: 'tek satır olmalı' })
  return out
}

export const renderedLength = (tpl: string, vars: { first_name: string; city: string } | null) =>
  runeLength(vars ? render(tpl.trim(), vars) : tpl.trim())
