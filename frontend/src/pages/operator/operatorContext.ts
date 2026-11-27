import { createContext, useContext } from 'react';
import type { Operator } from '../../api/types';

/** Вошедший сотрудник; задаёт RequireOperator */
export const OperatorContext = createContext<{ operator: Operator; setOperator: (op: Operator) => void } | null>(null);

export function useOperator(): Operator {
  const ctx = useContext(OperatorContext);
  if (!ctx) throw new Error('useOperator — только внутри RequireOperator');
  return ctx.operator;
}

export function useSetOperator() {
  return useContext(OperatorContext)!.setOperator;
}
