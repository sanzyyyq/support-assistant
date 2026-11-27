import { useLayoutEffect, useRef, type KeyboardEvent } from 'react';
import { Paperclip, Send } from 'lucide-react';

interface Props {
  value: string;
  onChange: (value: string) => void;
  onSubmit: (text: string) => void;
  placeholder?: string;
  disabled?: boolean;
  autoFocus?: boolean;
  /** Вызывается до встроенной обработки клавиш (например, Tab для подсказки) */
  onKeyDown?: (e: KeyboardEvent<HTMLTextAreaElement>) => void;
}

export function Composer({ value, onChange, onSubmit, placeholder = 'Напишите сообщение…', disabled, autoFocus, onKeyDown }: Props) {
  const ref = useRef<HTMLTextAreaElement>(null);

  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    el.style.height = 'auto';
    el.style.height = `${Math.min(el.scrollHeight, 160)}px`;
  }, [value]);

  const submit = () => {
    const text = value.trim();
    if (!text || disabled) return;
    onSubmit(text);
    ref.current?.focus();
  };

  return (
    <form
      className="composer"
      onSubmit={(e) => {
        e.preventDefault();
        submit();
      }}
    >
      <button type="button" className="composer__attach" aria-label="Прикрепить файл" title="Вложения появятся позже">
        <Paperclip size={20} />
      </button>
      <label className="sr-only" htmlFor="composer-input">
        Сообщение
      </label>
      <textarea
        id="composer-input"
        ref={ref}
        rows={1}
        value={value}
        placeholder={placeholder}
        disabled={disabled}
        autoFocus={autoFocus}
        onChange={(e) => onChange(e.target.value)}
        onKeyDown={(e) => {
          onKeyDown?.(e);
          if (e.defaultPrevented) return;
          if (e.key === 'Enter' && !e.shiftKey && !e.nativeEvent.isComposing) {
            e.preventDefault();
            submit();
          }
        }}
      />
      <button type="submit" className="composer__send" disabled={!value.trim() || disabled} aria-label="Отправить">
        <Send size={18} />
      </button>
    </form>
  );
}
