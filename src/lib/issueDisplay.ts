// Display only. Keep the complete issue for requests, comparisons and settlement.
export function formatIssue(value: string | null | undefined): string {
 if (!value) return '—'
 const issue = value.trim()
 const dated = /^(?:19|20)\d{2}(?:0[1-9]|1[0-2])(?:0[1-9]|[12]\d|3[01])(\d{1,6})$/.exec(issue)
 if (dated) return dated[1]!.padStart(6, '0')
 return /^\d{1,6}$/.test(issue) ? issue.padStart(6, '0') : issue
}
