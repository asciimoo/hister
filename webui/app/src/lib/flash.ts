import { tick } from 'svelte';
import { toast } from '@nogon/components/ui/sonner';

const flashMessageKey = 'histerFlashMessage';

type FlashMessage = {
  message: string;
  variant?: 'success' | 'info' | 'error';
};

export function setFlashMessage(
  message: string,
  variant: FlashMessage['variant'] = 'success',
): void {
  sessionStorage.setItem(flashMessageKey, JSON.stringify({ message, variant }));
}

export async function showFlashMessage(): Promise<boolean> {
  const rawMessage = sessionStorage.getItem(flashMessageKey);
  if (!rawMessage) return false;

  await tick();

  let flash: FlashMessage;
  try {
    flash = JSON.parse(rawMessage) as FlashMessage;
  } catch {
    flash = { message: rawMessage, variant: 'success' };
  }

  switch (flash.variant) {
    case 'error':
      toast.error(flash.message);
      break;
    case 'info':
      toast.info(flash.message);
      break;
    default:
      toast.success(flash.message);
  }
  sessionStorage.removeItem(flashMessageKey);
  return true;
}
