// Simple inline stroke icons.
type P = { size?: number };

export function PlusIcon({ size = 14 }: P) {
  return (
    <svg aria-hidden="true" width={size} height={size} viewBox="0 0 14 14" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round">
      <path d="M7 2v10M2 7h10" />
    </svg>
  );
}

export function CheckIcon({ size = 10 }: P) {
  return (
    <svg aria-hidden="true" width={size} height={size} viewBox="0 0 10 10" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
      <path d="M2 5.2l2 2 4-4.4" />
    </svg>
  );
}

export function BackIcon({ size = 14 }: P) {
  return (
    <svg aria-hidden="true" width={size} height={size} viewBox="0 0 14 14" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round">
      <path d="M8.5 3L4.5 7l4 4" />
    </svg>
  );
}

export function PencilIcon({ size = 13 }: P) {
  return (
    <svg aria-hidden="true" width={size} height={size} viewBox="0 0 14 14" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round">
      <path d="M9.5 2.5l2 2L5 11l-2.6.6L3 9z" />
    </svg>
  );
}

export function CloseIcon({ size = 10 }: P) {
  return (
    <svg aria-hidden="true" width={size} height={size} viewBox="0 0 10 10" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round">
      <path d="M2.5 2.5l5 5M7.5 2.5l-5 5" />
    </svg>
  );
}

export function MoreIcon({ size = 14 }: P) {
  return (
    <svg aria-hidden="true" width={size} height={size} viewBox="0 0 14 14" fill="currentColor">
      <circle cx="3" cy="7" r="1.2" />
      <circle cx="7" cy="7" r="1.2" />
      <circle cx="11" cy="7" r="1.2" />
    </svg>
  );
}
