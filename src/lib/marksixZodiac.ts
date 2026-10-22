const animals = ['鼠', '牛', '虎', '兔', '龙', '蛇', '马', '羊', '猴', '鸡', '狗', '猪']
let calendar: Intl.DateTimeFormat | undefined

// Historical results use their own draw date, with the same Beijing lunar-year
// boundary as settlement; never derive old results from the current year.
export function markSixLunarYear(drawAt: string): number | null {
 try {
  calendar ??= new Intl.DateTimeFormat('zh-CN-u-ca-chinese', {timeZone:'Asia/Shanghai', year:'numeric'})
  const year = calendar.formatToParts(new Date(drawAt)).find(part => (part.type as string) === 'relatedYear')?.value
  return year ? Number(year) : null
 } catch { return null }
}

export function markSixZodiacLabel(number: number, year: number | null): string {
 if (year === null || !Number.isInteger(year) || !Number.isInteger(number) || number < 1 || number > 49) return ''
 return animals[((year - 4 - (number - 1)) % 12 + 12) % 12] ?? ''
}
