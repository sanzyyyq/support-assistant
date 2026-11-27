import type { ButtonHTMLAttributes, ReactNode } from 'react';
import type { TicketStatus } from '../api/types';

type ButtonProps = ButtonHTMLAttributes<HTMLButtonElement> & {
  variant?: 'primary' | 'secondary' | 'soft' | 'ghost';
  size?: 'md' | 'sm';
  icon?: ReactNode;
  block?: boolean;
};

export function Button({ variant = 'secondary', size = 'md', icon, block, className = '', children, type = 'button', ...rest }: ButtonProps) {
  return (
    <button type={type} className={`btn btn--${variant} btn--${size}${block ? ' btn--block' : ''} ${className}`} {...rest}>
      {icon}
      {children}
    </button>
  );
}

type IconButtonProps = ButtonHTMLAttributes<HTMLButtonElement> & {
  label: string;
  variant?: 'secondary' | 'ghost' | 'soft';
};

export function IconButton({ label, variant = 'secondary', className = '', children, type = 'button', ...rest }: IconButtonProps) {
  return (
    <button type={type} aria-label={label} title={label} className={`icon-btn icon-btn--${variant} ${className}`} {...rest}>
      {children}
    </button>
  );
}

const STATUS: Record<TicketStatus, string> = {
  ai: 'ИИ-агент',
  waiting: 'Ждёт оператора',
  operator: 'Оператор',
  closed: 'Закрыт',
};

export function StatusBadge({ status }: { status: TicketStatus }) {
  return (
    <span className={`badge badge--${status}`}>
      <i className="badge__dot" />
      {STATUS[status]}
    </span>
  );
}

export function Avatar({ tone = 'neutral', size = 32, children }: { tone?: 'brand' | 'success' | 'neutral' | 'brand-soft'; size?: number; children: ReactNode }) {
  return (
    <span className={`avatar avatar--${tone}`} style={{ width: size, height: size, fontSize: size >= 44 ? 16 : undefined }}>
      {children}
    </span>
  );
}
