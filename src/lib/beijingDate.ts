// Stable YYYY-MM-DD independent of device timezone and locale support.
export const beijingToday = (now=Date.now()) => new Date(now+8*60*60*1000).toISOString().slice(0,10)
