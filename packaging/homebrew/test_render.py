import unittest
from render import render


class FormulaTest(unittest.TestCase):
    checksums = "a" * 64 + "  mav-darwin-arm64\n" + "b" * 64 + "  mav-darwin-amd64\n"

    def test_assets_dependency_and_version_are_published(self):
        formula = render("v0.34.0", self.checksums)
        self.assertIn('version "0.34.0"', formula)
        self.assertIn('depends_on "cameroncooke/axe/axe"', formula)
        self.assertIn('sha256 "' + "a" * 64 + '"', formula)
        self.assertIn('sha256 "' + "b" * 64 + '"', formula)
        self.assertNotIn("@", formula)

    def test_rejects_untrusted_or_incomplete_release_inputs(self):
        for tag, checksums in [
            ('v0.34.0"; system("cmd")', self.checksums),
            ("v0.34.0", "a  mav-darwin-arm64\n"),
            ("v0.34.0", self.checksums.splitlines()[0]),
            ("v0.34.0", self.checksums + self.checksums),
        ]:
            with self.assertRaises((ValueError, KeyError)):
                render(tag, checksums)


if __name__ == "__main__":
    unittest.main()
