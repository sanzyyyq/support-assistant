import { Check, CircleDashed } from 'lucide-react';
import type { Ticket } from '../api/types';
import { hasRequirements, requirementRows } from '../lib/domain';

/** «Что я уже знаю» — какие требования агент собрал, а какие ещё уточнит */
export function RequirementsBar({ ticket }: { ticket: Ticket }) {
  if (!hasRequirements(ticket.requirements) || ticket.scenario === 'question') return null;
  const rows = requirementRows(ticket.requirements, ticket.scenario);
  const filled = rows.filter((r) => r.value).length;
  return (
    <div className="reqbar" aria-label="Собранные требования">
      <span className="reqbar__title">
        Учитываю {filled}/{rows.length}:
      </span>
      {rows.map((r) => (
        <span key={r.slot} className={`reqbar__chip${r.value ? ' is-done' : ''}${ticket.pendingSlot === r.slot ? ' is-pending' : ''}`}>
          {r.value ? <Check size={12} aria-hidden /> : <CircleDashed size={12} aria-hidden />}
          {r.value ? r.value : r.label}
        </span>
      ))}
    </div>
  );
}
