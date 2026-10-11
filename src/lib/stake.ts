export const defaultStakes = [1000, 2000, 5000, 10000, 20000, 50000]
export function parseStake(text: string): number {
  if (!/^\d+(\.\d{1,2})?$/.test(text.trim())) return 0
  const value = Math.round(Number(text) * 100)
  return Number.isSafeInteger(value) && value > 0 && value <= 100000000 ? value : 0
}
export const stakeText = (cents: number) => String(cents / 100)
