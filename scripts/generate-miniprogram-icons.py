"""Generate the mini-program's local tab icons (Pillow, development only)."""
from pathlib import Path
from PIL import Image, ImageDraw

ROOT = Path(__file__).resolve().parents[1] / 'miniprogram/assets/tabs'
ROOT.mkdir(parents=True, exist_ok=True)
SCALE = 4

for name in ('home', 'bills', 'contracts', 'profile'):
    for selected in (False, True):
        image = Image.new('RGBA', (81 * SCALE, 81 * SCALE))
        draw = ImageDraw.Draw(image)
        color = '#245c45' if selected else '#68766d'
        def line(points, width=4):
            draw.line([(round(x*SCALE), round(y*SCALE)) for x,y in points], fill=color, width=width*SCALE, joint='curve')
        def rect(bounds, radius=3):
            draw.rounded_rectangle(tuple(round(v*SCALE) for v in bounds), radius=radius*SCALE, outline=color, width=4*SCALE)
        if name == 'home':
            line([(12,36),(40,12),(68,36)])
            line([(20,32),(20,66),(60,66),(60,32)])
            rect((33,45,47,66),1)
        elif name == 'bills':
            line([(21,15),(59,15),(59,67),(50,62),(40,67),(30,62),(21,67),(21,15)])
            line([(30,29),(50,29)])
            line([(30,41),(50,41)])
            line([(30,53),(45,53)])
        elif name == 'contracts':
            line([(20,13),(49,13),(62,26),(62,67),(20,67),(20,13)])
            line([(48,14),(48,28),(61,28)])
            line([(29,39),(52,39)])
            line([(29,49),(52,49)])
            line([(29,59),(45,59)])
        else:
            draw.ellipse((28*SCALE,12*SCALE,53*SCALE,37*SCALE),outline=color,width=4*SCALE)
            draw.arc((16*SCALE,43*SCALE,65*SCALE,88*SCALE),180,360,fill=color,width=4*SCALE)
            line([(17,65),(17,68),(64,68),(64,65)])
        image = image.resize((81,81),Image.Resampling.LANCZOS)
        image.save(ROOT / (name + ('-active' if selected else '') + '.png'),optimize=True)
